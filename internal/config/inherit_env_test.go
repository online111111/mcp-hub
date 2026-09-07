package config

import (
	"reflect"
	"testing"
)

func TestSafeInheritedEnvPOSIX(t *testing.T) {
	base := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/home/test",
		"LANG=en_US.UTF-8",
		"LC_ALL=C",
		"TMPDIR=/tmp",
		"SSL_CERT_FILE=/etc/ssl/certs/ca.pem",
		"AWS_SECRET_ACCESS_KEY=secret",
		"GITHUB_TOKEN=secret",
		"OPENAI_API_KEY=secret",
		"HTTP_PROXY=http://user:pass@example.invalid",
		"SSH_AUTH_SOCK=/tmp/agent.sock",
		"XDG_CONFIG_HOME=/home/test/.config",
	}
	want := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"HOME=/home/test",
		"LANG=en_US.UTF-8",
		"LC_ALL=C",
		"TMPDIR=/tmp",
		"SSL_CERT_FILE=/etc/ssl/certs/ca.pem",
	}
	if got := safeInheritedEnv(base, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("safeInheritedEnv() = %#v, want %#v", got, want)
	}
}

func TestSafeInheritedEnvWindowsIsCaseInsensitive(t *testing.T) {
	base := []string{
		"Path=C:\\Windows\\System32",
		"SystemRoot=C:\\Windows",
		"ComSpec=C:\\Windows\\System32\\cmd.exe",
		"PATHEXT=.COM;.EXE;.BAT;.CMD",
		"USERPROFILE=C:\\Users\\test",
		"LocalAppData=C:\\Users\\test\\AppData\\Local",
		"APPDATA=C:\\Users\\test\\AppData\\Roaming",
		"TEMP=C:\\Temp",
		"Azure_Client_Secret=secret",
		"HTTPS_PROXY=http://user:pass@example.invalid",
	}
	want := base[:8]
	if got := safeInheritedEnv(base, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("safeInheritedEnv() = %#v, want %#v", got, want)
	}
}
