package server

import (
	"crypto/rand"

	"github.com/sandcorporation/switchboard/internal/config"
	"github.com/sandcorporation/switchboard/internal/secrets"
)

// loadSecrets sets the key that seals secret values (#525): the parsed
// SWITCHBOARD_SECRETS_KEY, else the key file in dir (the database's folder),
// made on first start. Using the file logs one line, never the key.
func (s *Server) loadSecrets(dir string, logf func(string, ...any)) error {
	k, fromFile, err := secrets.Load(s.secretsRaw, dir)
	if err != nil {
		return err
	}
	if fromFile {
		logf("secrets: key file in use (%s unset)", config.EnvPrefix+config.SecretsKeyName)
	}
	s.secrets = k
	return nil
}

// secretsKey is the key that seals secret values. run loads it before
// serving; a server built without run (tests) gets a random key kept in
// memory on first use.
func (s *Server) secretsKey() *secrets.Key {
	s.secretsOnce.Do(func() {
		if s.secrets != nil {
			return
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			panic(err)
		}
		k, err := secrets.NewKey(raw)
		if err != nil {
			panic(err)
		}
		s.secrets = k
	})
	return s.secrets
}
