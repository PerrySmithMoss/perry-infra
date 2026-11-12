package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// rotate preserves all other users and adds a second password hash. retire keeps
// only the password from URL_FILE after the new credential is verified in use.
func rotate(user, aclPath, urlPath, directory string, retire bool) error {
	if user != "im_listening" && user != "infra_probe" && user != "admin" && user != "netdata" {
		return errors.New("rotation requires a named application/admin URL user")
	}
	acl, err := os.ReadFile(aclPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(urlPath)
	if err != nil {
		return err
	}
	u, err := url.Parse(strings.TrimSpace(string(data)))
	if err != nil || u.User == nil || u.User.Username() != user || u.Host != "perry-infra_redis:6379" {
		return errors.New("URL does not match the selected shared Redis user")
	}
	password, ok := u.User.Password()
	if !ok || password == "" {
		return errors.New("URL requires a password")
	}
	oldHash := fmt.Sprintf("#%x", sha256.Sum256([]byte(password)))
	if !retire {
		var value [32]byte
		if _, err := rand.Read(value[:]); err != nil {
			return err
		}
		password = hex.EncodeToString(value[:])
		u.User = url.UserPassword(user, password)
	}
	newHash := fmt.Sprintf("#%x", sha256.Sum256([]byte(password)))
	lines, found := strings.Split(string(acl), "\n"), false
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "user" || fields[1] != user {
			continue
		}
		if found {
			return errors.New("duplicate ACL user")
		}
		found = true
		var rules []string
		matches := false
		for _, field := range fields {
			if field == oldHash {
				matches = true
			}
			if strings.HasPrefix(field, "#") && retire {
				continue
			}
			rules = append(rules, field)
		}
		if !matches {
			return errors.New("current URL password does not match the ACL")
		}
		lines[index] = strings.Join(append(rules, newHash), " ")
	}
	if !found {
		return errors.New("selected user is absent from the ACL")
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(directory, "users.acl"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "url"), []byte(u.String()), 0o600)
}
