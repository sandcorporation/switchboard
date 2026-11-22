package node_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/node"
	"github.com/sandcorporation/switchboard/internal/wire"
)

// TestLinkAnswersFileFrames: the node says it answers file frames, sends a
// read's bytes in a FileData frame before the file-result frame, and
// answers a refused path with a result alone.
func TestLinkAnswersFileFrames(t *testing.T) {
	r := newRig(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := rec("a")
	rc.Directory, rc.Desired = dir, node.DesiredStopped
	type got struct {
		header string
		frames []string // "bin:<ref>:<bytes>" or "res:<ref>:<code>:<length>"
	}
	out := make(chan got, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ws, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := req.Context()
		g := got{header: req.Header.Get(config.HeaderPrefix + config.NodeFilesHeaderName)}
		_ = wsjson.Write(ctx, ws, wire.NodeFrame{Type: wire.NodeAssign, Sessions: []wire.HostedSession{rc}})
		_ = wsjson.Write(ctx, ws, wire.NodeFrame{Type: wire.NodeFileType, ID: "a", File: &wire.NodeFile{Ref: "f1", Op: wire.FileRead, Path: "a.txt"}})
		_ = wsjson.Write(ctx, ws, wire.NodeFrame{Type: wire.NodeFileType, ID: "a", File: &wire.NodeFile{Ref: "f2", Op: wire.FileRead, Path: "../x"}})
		for len(g.frames) < 3 {
			typ, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				kind, ref, data, _ := wire.DecodeTermFrame(b)
				if kind == wire.FileData {
					g.frames = append(g.frames, "bin:"+ref+":"+string(data))
				}
				continue
			}
			var f wire.NodeFrame
			if json.Unmarshal(b, &f) == nil && f.Type == wire.NodeFileResultType {
				g.frames = append(g.frames, "res:"+f.FileResult.Ref+":"+f.FileResult.Code+":"+string(rune('0'+f.FileResult.Length)))
			}
		}
		out <- g
		<-ctx.Done()
	}))
	defer srv.Close()
	runLink(t, &node.Link{Node: r.node(), Host: srv.URL, Token: "sbn_t", Interval: 10 * time.Millisecond, Logf: t.Logf,
		Sleep: func(ctx context.Context, d time.Duration) error { return ctx.Err() }})
	select {
	case g := <-out:
		if g.header != "1" {
			t.Errorf("files header %q, want 1", g.header)
		}
		pos := map[string]int{}
		for i, f := range g.frames {
			pos[f] = i + 1
		}
		if pos["bin:f1:hello"] == 0 || pos["res:f1::5"] == 0 || pos["bin:f1:hello"] > pos["res:f1::5"] || pos["res:f2:"+wire.FileRefused+":0"] == 0 {
			t.Fatalf("frames %q: want the bytes of f1 before its result, and f2 refused", g.frames)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no file answers")
	}
}

// TestLinkWritesFiles: the node says it writes, takes a write's bytes from
// the FileData frames before the write frame and answers with the file
// written; a write whose bytes are not its length writes nothing.
func TestLinkWritesFiles(t *testing.T) {
	r := newRig(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	rc := rec("a")
	rc.Directory, rc.Desired = dir, node.DesiredStopped
	type got struct {
		header string
		res    map[string]wire.NodeFileResult
	}
	out := make(chan got, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ws, err := websocket.Accept(w, req, nil)
		if err != nil {
			return
		}
		defer ws.CloseNow()
		ctx := req.Context()
		g := got{header: req.Header.Get(config.HeaderPrefix + config.NodeFileWriteHeaderName), res: map[string]wire.NodeFileResult{}}
		_ = wsjson.Write(ctx, ws, wire.NodeFrame{Type: wire.NodeAssign, Sessions: []wire.HostedSession{rc}})
		_ = ws.Write(ctx, websocket.MessageBinary, wire.EncodeTermFrame(wire.FileData, "w1", []byte("new ")))
		_ = ws.Write(ctx, websocket.MessageBinary, wire.EncodeTermFrame(wire.FileData, "w1", []byte("text")))
		_ = wsjson.Write(ctx, ws, wire.NodeFrame{Type: wire.NodeFileType, ID: "a", File: &wire.NodeFile{Ref: "w1", Op: wire.FileWrite, Path: "a.txt", Length: 8}})
		_ = wsjson.Write(ctx, ws, wire.NodeFrame{Type: wire.NodeFileType, ID: "a", File: &wire.NodeFile{Ref: "w2", Op: wire.FileWrite, Path: "a.txt", Length: 3}})
		for len(g.res) < 2 {
			typ, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var f wire.NodeFrame
			if typ == websocket.MessageText && json.Unmarshal(b, &f) == nil && f.Type == wire.NodeFileResultType {
				g.res[f.FileResult.Ref] = *f.FileResult
			}
		}
		out <- g
		<-ctx.Done()
	}))
	defer srv.Close()
	runLink(t, &node.Link{Node: r.node(), Host: srv.URL, Token: "sbn_t", Interval: 10 * time.Millisecond, Logf: t.Logf,
		Sleep: func(ctx context.Context, d time.Duration) error { return ctx.Err() }})
	select {
	case g := <-out:
		if g.header != "1" {
			t.Errorf("write header %q, want 1", g.header)
		}
		if w1 := g.res["w1"]; w1.Code != "" || w1.Stat == nil || w1.Stat.Size != 8 || w1.Stat.SHA256 == "" {
			t.Errorf("w1 = %+v", w1)
		}
		if w2 := g.res["w2"]; w2.Code != wire.FileFailed {
			t.Errorf("w2 (no bytes) = %+v", w2)
		}
		if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "new text" {
			t.Errorf("a.txt = %q", b)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no write answers")
	}
}
