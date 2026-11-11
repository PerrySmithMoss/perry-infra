// redis-admin generates private Swarm secret inputs without printing credentials.
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Include commands used inside the application's Lua and rate limiter scripts.
// Omit enumeration, database selection, pub/sub, and administrator commands.
const imCommands = "+ping +hello +client|setinfo +client|setname " +
	"+get +set +getdel +del +exists +expire +pexpire +pttl +incr " +
	"+multi +exec +eval +evalsha +script|load"
const probeCommands = "+ping +hello +client|setinfo +get +set +del"

func secretInputs() (map[string]string, error) {
	passwords := map[string]string{}
	for _, name := range []string{"admin", "healthcheck", "im_listening", "infra_probe"} {
		var value [32]byte
		if _, err := rand.Read(value[:]); err != nil {
			return nil, err
		}
		passwords[name] = hex.EncodeToString(value[:])
	}
	user := func(name, rules string) string {
		digest := sha256.Sum256([]byte(passwords[name]))
		return fmt.Sprintf("user %s reset on #%x resetchannels %s\n", name, digest, rules)
	}
	acl := "user default reset off\n" +
		user("admin", "~* +@all") +
		user("healthcheck", "-@all +ping") +
		user("im_listening", "~im-listening:* -@all "+imCommands) +
		user("infra_probe", "~infra-probe:* -@all "+probeCommands)
	url := func(name string) string {
		return "redis://" + name + ":" + passwords[name] + "@perry-infra_redis:6379/0"
	}
	return map[string]string{
		"redis_users_v1":                acl,
		"redis_healthcheck_password_v1": passwords["healthcheck"],
		"redis_admin_url_v1":            url("admin"),
		"redis_probe_url_v1":            url("infra_probe"),
		"im_listening_redis_url_v2":     url("im_listening"),
	}, nil
}

func credentials(directory string) error {
	values, err := secretInputs()
	if err != nil {
		return err
	}
	// Refuse an existing directory: retries must never silently rotate passwords.
	if err := os.Mkdir(directory, 0o700); err != nil {
		return err
	}
	for name, value := range values {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0o600); err != nil {
			return errors.New("writing credential inputs failed; remove the incomplete private directory before retrying")
		}
	}
	return nil
}

func run(args []string) error {
	if len(args) == 5 && args[0] == "rotate" {
		return rotate(args[1], args[2], args[3], args[4], false)
	}
	if len(args) == 5 && args[0] == "retire" {
		return rotate(args[1], args[2], args[3], args[4], true)
	}
	if len(args) == 3 && args[0] == "migrate" {
		return migrate(args[1], args[2])
	}
	if len(args) == 3 && args[0] == "resume" {
		return resume(args[1], args[2])
	}
	if len(args) == 2 && args[0] == "verify" {
		return verifyProduction(args[1])
	}
	if len(args) == 1 && args[0] == "rollback" {
		return rollback()
	}
	if len(args) == 2 && args[0] == "credentials" && strings.TrimSpace(args[1]) != "" {
		return credentials(args[1])
	}
	return errors.New("usage: redis-admin credentials NEW_PRIVATE_DIRECTORY | migrate|resume CHECKOUT BROKER_BINARY | verify CHECKOUT | rollback | rotate|retire USER ACL_FILE URL_FILE NEW_PRIVATE_DIRECTORY")
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Redis administrator operation completed; no credentials printed.")
}
