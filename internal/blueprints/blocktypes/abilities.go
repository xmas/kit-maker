package blocktypes

import "slices"

// Ability is one thing a block lets a sandbox do (or stops it doing), for
// filtering the library. Worked out from the block's content, not its type
// alone: a network block that only allows GET is "read-only network".
type Ability struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Abilities in the order the library lists them.
var Abilities = []Ability{
	{"network", "Network access"},
	{"readonly", "Read-only network"},
	{"install-network", "Install-time network"},
	{"deny", "Blocks hosts"},
	{"secrets", "Uses secrets"},
	{"ssh", "SSH agent"},
	{"commands", "Runs commands"},
	{"files", "Writes files"},
	{"storage", "Storage"},
	{"host-files", "Host folders"},
	{"ports", "Opens ports"},
	{"devices", "Devices"},
	{"privileged", "Privileged"},
	{"resources", "Resources"},
	{"agent", "Agent behaviour"},
	{"software", "Software"},
	{"metadata", "Metadata"},
	{"identity", "Identity"},
	{"host", "Host features"},
	{"custom", "Custom capability"},
}

var readMethods = []string{"GET", "HEAD", "OPTIONS"}

// AbilitiesOf lists what a block of type key with payload p can do.
func AbilitiesOf(key string, p map[string]any) []string {
	var out []string
	add := func(a ...string) {
		for _, x := range a {
			if !slices.Contains(out, x) {
				out = append(out, x)
			}
		}
	}
	switch key {
	case NetworkRules:
		allows, readOnly := 0, true
		for phase, v := range p {
			rules, _ := v.(map[string]any)
			for _, it := range asList(rules["allow"]) {
				allows++
				if phase == "install" {
					add("install-network")
				}
				m, _ := it.(map[string]any)
				methods := strings_(m["methods"])
				if len(methods) == 0 {
					readOnly = false
				}
				for _, meth := range methods {
					if !slices.Contains(readMethods, meth) {
						readOnly = false
					}
				}
			}
			if len(asList(rules["deny"])) > 0 {
				add("deny")
			}
		}
		if allows > 0 {
			if readOnly {
				add("readonly")
			} else {
				add("network")
			}
		}
	case Credential, "oauth-credential":
		add("secrets")
	case "ssh-agent":
		add("ssh")
	case "install-steps", "startup-steps", "interactive":
		add("commands")
	case "files":
		add("files")
	case "volume":
		add("storage")
	case "host-mount":
		add("host-files")
	case "port":
		add("ports")
	case "usb-device":
		add("devices")
	case "privileged":
		add("privileged")
	case "resources":
		add("resources")
	case "agent-launch", "agent-context", "agent-sessions", "interactive-sessions", "acp", "agent-skills", "agent-skill":
		add("agent")
	case "dockerfile", "kits":
		add("software")
	case "about", "dependencies", "args":
		add("metadata")
	case "git-identity":
		add("identity")
	case "long-running", "kit-registry":
		add("host")
	case RawCapability:
		add("custom")
	}
	return out
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}
