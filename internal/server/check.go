package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sandcorporation/switchboard/internal/adminauth"
	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/insight"
	"github.com/sandcorporation/switchboard/internal/presence"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// startSettings applies to s every start-up decision that reads only the
// settings: each refusal run makes before it opens the database or binds a
// listener (#428). run and Check both call it, so the deploy's check refuses
// exactly what the server would. It reports every refusal, not just the
// first.
func startSettings(s *Server, getenv func(string) string, serverVersion string) error {
	var errs []error
	add := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	on, err := hostingStart(getenv)
	add(err)
	s.hosting = on
	proxy, _ := config.EnvAliasFrom(getenv, "ADMIN_PROXY_TOKEN")
	s.admin = adminauth.Config{ProxyToken: proxy}
	public, _ := config.EnvAliasFrom(getenv, "PUBLIC_URL")
	s.publicURL = strings.TrimRight(public, "/")
	if v, from := config.EnvAliasFrom(getenv, "SSE_KEEPALIVE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			add(errors.New(from + " must be a positive duration such as 20s"))
		}
		s.mailCfg.KeepAlive = d
	}
	if v, from := config.EnvAliasFrom(getenv, "POLL_MAX"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			add(errors.New(from + " must be a positive whole number of seconds such as 55"))
		}
		s.mailCfg.PollMax = time.Duration(n) * time.Second
	}
	if v, from := config.EnvAliasFrom(getenv, "SLOT_LEASE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Second {
			add(errors.New(from + " must be a duration of at least 1s such as 3m"))
		}
		s.presenceCfg.SlotLease = d
	}
	add(presence.Configure(&s.presenceCfg, getenv))
	configureCLIMin(s, getenv, serverVersion, log.Printf)
	add(configureStall(s, getenv))
	add(configureAlerts(s, getenv))
	s.discord = insight.ConfigureDiscord(getenv)
	add(adminauth.Configure(&s.admin, getenv))
	s.terminal, err = terminalSettings(getenv)
	add(err)
	add(checkListenAddr(listenAddr(getenv)))
	return errors.Join(errs...)
}

// listenAddr is LISTEN_ADDR, else the default.
func listenAddr(getenv func(string) string) string {
	if v := getenv("LISTEN_ADDR"); v != "" {
		return v
	}
	return defaultListenAddr
}

// checkListenAddr refuses a LISTEN_ADDR the listener could not bind for its
// form alone (no host:port, or no port). The message leaves the value out.
func checkListenAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err == nil {
		_, err = net.LookupPort("tcp", port)
	}
	if err != nil {
		return errors.New("LISTEN_ADDR must be host:port or :port such as :8080")
	}
	return nil
}

// checkHealthTimeout bounds Check's request to SWITCHBOARD_PUBLIC_URL.
const checkHealthTimeout = 10 * time.Second

// Check runs the start-up settings decisions of the server (startSettings)
// without opening the database or binding a listener, for `switchboard serve
// --check` (#428). It prints each refusal to out and returns 1 when there is
// one, else 0. When SWITCHBOARD_PUBLIC_URL is set it also asks that address
// for /health (certificate checked) and prints a warning when no switchboard
// answers there; that alone does not fail the check, since on a first deploy
// the proxy in front may not exist yet. No setting's value is printed.
func Check(v string, getenv func(string) string, out io.Writer, client *http.Client) int {
	// The settings functions log the start lines a serve logs; the check
	// prints only its verdict.
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(io.Discard)
	s := &Server{tokens: map[string]string{}, done: make(chan struct{})}
	err := startSettings(s, getenv, v)
	log.SetOutput(prev)
	log.SetFlags(flags)
	if err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			fmt.Fprintln(out, "settings: refused:", line)
		}
		fmt.Fprintln(out, "settings: the server would refuse to start")
		return 1
	}
	if s.publicURL != "" {
		_, from := config.EnvAliasFrom(getenv, "PUBLIC_URL")
		if werr := probeHealth(client, s.publicURL); werr != nil {
			fmt.Fprintf(out, "settings: warning: %s: %v\n", from, werr)
		} else {
			fmt.Fprintf(out, "settings: %s answers /health\n", from)
		}
	}
	fmt.Fprintln(out, "settings: ok")
	return 0
}

// probeHealth asks base/health and reports why no switchboard answered. The
// error never includes the address.
func probeHealth(client *http.Client, base string) error {
	if client == nil {
		client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkHealthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return errors.New("not a URL")
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("/health did not answer: " + requestFailure(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/health answered %d", resp.StatusCode)
	}
	var h wire.ServerVersionInfo
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&h) != nil || !h.OK {
		return errors.New("/health answered, but not as a switchboard server")
	}
	return nil
}

// requestFailure names why a request failed without the address, which the
// net and TLS errors carry.
func requestFailure(err error) string {
	var dns *net.DNSError
	var certInvalid x509.CertificateInvalidError
	var certHost x509.HostnameError
	var certAuthority x509.UnknownAuthorityError
	var certVerify *tls.CertificateVerificationError
	var op *net.OpError
	switch {
	case errors.As(err, &certVerify), errors.As(err, &certInvalid), errors.As(err, &certHost), errors.As(err, &certAuthority):
		return "the certificate check failed"
	case errors.As(err, &dns):
		return "the host name does not resolve"
	case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
		return "no answer within " + checkHealthTimeout.String()
	case errors.As(err, &op):
		return "cannot connect"
	}
	return "the request failed"
}

// isTimeout reports a network timeout.
func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}
