package pgtest

import (
	"fmt"
	"net"
	"os"
	"path/filepath"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

type EmbeddedServer struct {
	db *embeddedpostgres.EmbeddedPostgres

	Host     string
	Port     int
	User     string
	Password string
	Database string
}

func StartEmbedded(baseDir string) (*EmbeddedServer, error) {
	port, err := freeTCPPort()
	if err != nil {
		return nil, err
	}
	cachePath := filepath.Join(baseDir, "cache")
	if userCacheDir, err := os.UserCacheDir(); err == nil && userCacheDir != "" {
		cachePath = filepath.Join(userCacheDir, "cihealth", "embedded-postgres")
	}
	cfg := embeddedpostgres.DefaultConfig().
		// embedded-postgres v1.34.0 advertises V18 as 18.3.0, but the
		// downloadable binary set does not currently include that release.
		Version(embeddedpostgres.V17).
		Port(uint32(port)).
		RuntimePath(filepath.Join(baseDir, "runtime")).
		CachePath(cachePath).
		DataPath(filepath.Join(baseDir, "data"))

	db := embeddedpostgres.NewDatabase(cfg)
	if err := db.Start(); err != nil {
		return nil, fmt.Errorf("start embedded postgres: %w", err)
	}
	return &EmbeddedServer{
		db:       db,
		Host:     "localhost",
		Port:     port,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
	}, nil
}

func (s *EmbeddedServer) Stop() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Stop()
}

func freeTCPPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate free tcp port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}
