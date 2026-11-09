package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var snapshotFiles = [...]string{
	"docker-compose.stack.yaml",
	"postgres/pg_hba.conf",
	"docker-proxy/haproxy.cfg",
}

func Fingerprint(root string) (string, error) {
	hash := sha256.New()
	for _, name := range snapshotFiles {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			return "", err
		}
		// Keep the original wire format: path, NUL, raw file bytes, NUL.
		io.WriteString(hash, name)
		hash.Write([]byte{0})
		hash.Write(content)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

var acmeEmailPattern = regexp.MustCompile(`^[A-Za-z0-9_.+@-]+$`)

// ACMEEnv extracts the first ACME_EMAIL entry from Traefik's Docker environment.
// The installer sources this assignment as shell text.
func ACMEEnv(input io.Reader) (string, error) {
	raw, err := io.ReadAll(input)
	var env []string
	if err != nil || json.Unmarshal(raw, &env) != nil {
		return "", errors.New("invalid Traefik environment")
	}
	for _, entry := range env {
		if value, found := strings.CutPrefix(entry, "ACME_EMAIL="); found {
			if !acmeEmailPattern.MatchString(value) {
				return "", errors.New("invalid ACME_EMAIL")
			}
			return entry, nil
		}
	}
	return "", errors.New("missing ACME_EMAIL")
}
