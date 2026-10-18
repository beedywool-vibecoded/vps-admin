package assistant

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		cmd  string
		want Kind
	}{
		// read-only
		{"df -h", Read},
		{"journalctl -u caddy -n 50 --no-pager", Read},
		{"systemctl status wg-quick@wg0", Read},
		{"docker ps -a", Read},
		{"docker logs --tail 100 stabilite-api-1", Read},
		{"docker compose ls", Read},
		{"ss -tlnp | grep 443", Read},
		{"ping -c 3 10.10.0.2", Read},
		{"curl -sI https://example.com", Read},
		{"find /var/www -name '*.html'", Read},
		{"wg show", Read},
		{"ufw status verbose", Read},
		{"ip a", Read},
		{"timeout 5 ping -c 1 1.1.1.1", Read},
		{"cat /etc/caddy/Caddyfile", Read},

		// needs approval
		{"systemctl restart wg-quick@wg0", Change},
		{"sudo systemctl restart caddy", Change},
		{"docker restart stabilite-api-1", Change},
		{"docker compose up -d", Change},
		{"apt install htop", Change},
		{"echo hi > /tmp/x", Change},
		{"ls; reboot", Change},
		{"cat $(echo /etc/hosts)", Change},
		{"curl -X POST https://x", Change},
		{"curl -o /tmp/x https://x", Change},
		{"find / -name x -delete", Change},
		{"rm /tmp/old.log", Change},
		{"docker volume ls", Read},
		{"docker network create x", Change},
		{"ip link set eth0 down", Change},
		{"caddy reload", Change},
		{"reboot", Change},

		// never
		{"docker volume rm data", Blocked},
		{"docker system prune -a --volumes", Blocked},
		{"docker compose down -v", Blocked},
		{"cat /etc/ssh/sshd_config", Blocked},
		{"sudo ufw allow 8080/tcp", Blocked},
		{"ufw disable", Blocked},
		{"nft flush ruleset", Blocked},
		{"iptables -F", Blocked},
		{"systemctl stop ssh", Blocked},
		{"cat ~/.ssh/id_ed25519", Blocked},
		{"cat /var/lib/vps-admin/secret.key", Blocked},
		{"sudo cat /etc/shadow", Blocked},
		{"rm -rf /", Blocked},
		{"rm -rf /etc", Blocked},
		{"dd if=/dev/zero of=/dev/sda", Blocked},
		{"shutdown -h now", Blocked},
		{"", Blocked},
	}
	for _, c := range cases {
		if got, why := Classify(c.cmd); got != c.want {
			t.Errorf("%q: got %s (%s), want %s", c.cmd, got, why, c.want)
		}
	}
}
