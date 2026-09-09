package web

import (
	"testing"
)

func TestIsProtectedPathString(t *testing.T) {
	tests := []struct {
		path      string
		protected bool
	}{
		{"/", true},
		{".", true},
		{"", true},
		{"/bin", true},
		{"/bin/sh", true},
		{"/boot", true},
		{"/boot/vmlinuz", true},
		{"/dev", true},
		{"/dev/null", true},
		{"/etc", true},
		{"/etc/shadow", true},
		{"/etc/nginx/nginx.conf", true},
		{"/lib", true},
		{"/lib64", true},
		{"/lib32", true},
		{"/proc", true},
		{"/proc/1/cmdline", true},
		{"/root", true},
		{"/root/.ssh/authorized_keys", true},
		{"/sbin", true},
		{"/sbin/iptables", true},
		{"/sys", true},
		{"/usr", true},
		{"/usr/local/bin/app", true},
		{"/run", true},
		{"/run/systemd", true},

		// Path traversal attempts
		{"/var/www/../../etc/passwd", true},
		{"/home/ubuntu/../../root/.bashrc", true},
		{"etc/shadow", true},
		{"../etc/shadow", true},

		// Allowed user / app locations
		{"/var/www/html/index.php", false},
		{"/home/user/project/file.txt", false},
		{"/opt/myapp/config.yaml", false},
		{"/tmp/test.log", false},
		{"/srv/data/file.csv", false},
	}

	for _, tt := range tests {
		got := isProtectedPathString(tt.path)
		if got != tt.protected {
			t.Errorf("isProtectedPathString(%q) = %v; want %v", tt.path, got, tt.protected)
		}
	}
}
