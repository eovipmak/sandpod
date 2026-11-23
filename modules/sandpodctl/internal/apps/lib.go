// Package apps ports modules/customer-apps/scripts (lib.sh + 6 commands).
package apps

import (
	"fmt"
	"os"
	"time"

	"sandpodctl/internal/platform"
)

// Ctx mirrors lib.sh sourced state.
type Ctx struct {
	ServiceUser   string
	Container     string
	Home          string
	UID           int
	NginxConf     string
	SitesDir      string
	SupervisorDir string
}

// Load mirrors `source lib.sh`: validates, resolves home/uid, checks manager.
func Load(serviceUser, container string) (*Ctx, int) {
	if !platform.ValidServiceUser(serviceUser) {
		fmt.Fprintln(os.Stderr, "Invalid service user.")
		return nil, 2
	}
	if !platform.ValidContainer(container) {
		fmt.Fprintln(os.Stderr, "Invalid container name.")
		return nil, 2
	}
	home, uid, err := platform.LookupAccount(serviceUser)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unknown user %s.\n", serviceUser)
		return nil, 1
	}
	if home == "" || !platform.UserManagerUp(uid) {
		fmt.Fprintf(os.Stderr, "User manager for %s is down (enable-linger first).\n", serviceUser)
		return nil, 1
	}
	nginxConf := home + "/config/nginx.conf"
	if _, err := os.Stat(nginxConf); err != nil {
		fmt.Fprintf(os.Stderr, "Missing %s.\n", nginxConf)
		return nil, 1
	}
	return &Ctx{
		ServiceUser: serviceUser, Container: container,
		Home: home, UID: uid,
		NginxConf:     nginxConf,
		SitesDir:      home + "/config/sites",
		SupervisorDir: home + "/config/supervisor",
	}, 0
}

func (c *Ctx) containerExec(args ...string) error {
	return platform.RunAsUser(c.ServiceUser, c.Home, c.UID, "podman",
		append([]string{"exec", c.Container}, args...)...)
}

// NginxTestReload mirrors lib.sh (nginx -t + reload + 1s drain).
func (c *Ctx) NginxTestReload() error {
	if err := c.containerExec("nginx", "-t"); err != nil {
		return err
	}
	if err := c.containerExec("nginx", "-s", "reload"); err != nil {
		return err
	}
	time.Sleep(time.Second)
	return nil
}

// SupervisorRereadUpdate mirrors lib.sh.
func (c *Ctx) SupervisorRereadUpdate() error {
	if err := c.containerExec("supervisorctl", "reread"); err != nil {
		return err
	}
	return c.containerExec("supervisorctl", "update")
}

// EnsureAppDirs mirrors lib.sh.
func (c *Ctx) EnsureAppDirs() error {
	return platform.MkdirOwn(0755, c.ServiceUser, c.ServiceUser, c.SitesDir, c.SupervisorDir)
}
