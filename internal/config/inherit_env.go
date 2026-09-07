package config

import "strings"

var safeInheritedEnvNames = map[string]struct{}{
	"PATH":                {},
	"HOME":                {},
	"TMPDIR":              {},
	"TMP":                 {},
	"TEMP":                {},
	"LANG":                {},
	"LANGUAGE":            {},
	"TZ":                  {},
	"SSL_CERT_FILE":       {},
	"SSL_CERT_DIR":        {},
	"NODE_EXTRA_CA_CERTS": {},
	"REQUESTS_CA_BUNDLE":  {},
	"CURL_CA_BUNDLE":      {},
}

var safeInheritedWindowsEnvNames = map[string]struct{}{
	"PATHEXT":      {},
	"SYSTEMROOT":   {},
	"WINDIR":       {},
	"COMSPEC":      {},
	"USERPROFILE":  {},
	"HOMEDRIVE":    {},
	"HOMEPATH":     {},
	"APPDATA":      {},
	"LOCALAPPDATA": {},
	"PROGRAMDATA":  {},
	"SYSTEMDRIVE":  {},
}

// safeInheritedEnv returns only runtime-discovery, locale, temporary-directory,
// and trust-store variables needed by common stdio MCP runtimes. Credentials,
// proxy variables, agent sockets, cloud-provider variables, and other ambient
// process state are intentionally excluded. Any excluded variable can still be
// forwarded explicitly through server.env.
func safeInheritedEnv(base []string, windows bool) []string {
	if len(base) == 0 {
		return nil
	}

	out := make([]string, 0, len(base))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			continue
		}

		lookup := name
		if windows {
			lookup = strings.ToUpper(lookup)
		}

		_, allowed := safeInheritedEnvNames[lookup]
		if !allowed && strings.HasPrefix(lookup, "LC_") {
			allowed = true
		}
		if !allowed && windows {
			_, allowed = safeInheritedWindowsEnvNames[lookup]
		}
		if allowed {
			out = append(out, entry)
		}
	}
	return out
}
