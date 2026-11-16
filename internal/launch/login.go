package launch

import (
	"bytes"
	"encoding/xml"
)

// agentLabel names DevTopo's macOS LaunchAgent, and its file.
const agentLabel = "com.devtopo.app"

// launchAgent is the LaunchAgent plist that starts exe in the tray when the
// user logs in, without opening the browser.
func launchAgent(exe string) string {
	var esc bytes.Buffer
	_ = xml.EscapeText(&esc, []byte(exe))
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + agentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc.String() + `</string>
		<string>tray</string>
		<string>--no-open</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
}
