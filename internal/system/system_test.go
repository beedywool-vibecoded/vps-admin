package system

import "testing"

func TestParseSS(t *testing.T) {
	out := `tcp   LISTEN 0 4096 0.0.0.0:21558 0.0.0.0:* users:(("sshd",pid=1,fd=3))
tcp   LISTEN 0 4096 [::]:21558 [::]:* users:(("sshd",pid=1,fd=4))
tcp   LISTEN 0 4096 127.0.0.1:7171 0.0.0.0:* users:(("vps-admin",pid=2,fd=5))
udp   UNCONN 0 0 172.20.0.1:53 0.0.0.0:*
udp   UNCONN 0 0 172.15.0.1:9 0.0.0.0:*`
	ps := parseSS(out)
	if len(ps) != 4 {
		t.Fatalf("got %d ports: %+v", len(ps), ps)
	}
	byPort := map[int]Port{}
	for _, p := range ps {
		byPort[p.Port] = p
	}
	if p := byPort[21558]; !p.Public || p.Process != "sshd" {
		t.Errorf("ssh: %+v", p)
	}
	if byPort[7171].Public || byPort[53].Public {
		t.Error("loopback or private address marked public")
	}
	if !byPort[9].Public {
		t.Error("172.15/16 is not private")
	}
}

func TestNativeDropTable(t *testing.T) {
	both := "table inet filter {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\ntable ip filter {\n\tchain INPUT {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\n"
	if !nativeDropTable(both) {
		t.Error("native table not detected")
	}
	ufwOnly := "table ip filter {\n\tchain INPUT {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\n"
	if nativeDropTable(ufwOnly) {
		t.Error("ufw table counted as native")
	}
}

func TestCPUPercent(t *testing.T) {
	if got := cpuPercent(cpuTimes{idle: 100, total: 200}, cpuTimes{idle: 150, total: 300}); got != 50 {
		t.Errorf("got %v, want 50", got)
	}
	if got := cpuPercent(cpuTimes{total: 10}, cpuTimes{total: 10}); got != 0 {
		t.Errorf("no delta should give 0, got %v", got)
	}
}

func TestToolVersion(t *testing.T) {
	cases := []struct{ out, want string }{
		// Caddy writes its warnings on the same stream as the version.
		{"{\"level\":\"warn\",\"ts\":1792785636.9141853,\"msg\":\"unable to get instance ID\"}\nv2.10.0 h1:abc", "v2.10.0"},
		{"v2.7.6 h1:XYZ", "v2.7.6"},
		{"nginx version: nginx/1.24.0", "1.24.0"},
		{"certbot 2.1.0", "2.1.0"},
		{"", ""},
		{"something without a version", "something without a version"},
	}
	for _, c := range cases {
		if got := toolVersion(c.out); got != c.want {
			t.Errorf("toolVersion(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}
