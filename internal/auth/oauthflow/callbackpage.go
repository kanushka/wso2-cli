// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package oauthflow

import (
	"fmt"
	"html"
	"net/http"
	"strings"
)

// The four pages the loopback listener can serve. Each is one card on a plain
// page: what happened, one sentence about it, the product the login was for
// when the caller named one, and what to do next.
//
// The two rejected callbacks must not read like the accepted one. They differ
// by icon, by icon color, and by never using the word that means the login
// worked, because a person who lands on one after a stray or forged redirect
// has to be able to tell at a glance that nothing was authorized.
//
// Nothing from the query string reaches any of them: not the code, not the
// state, not the provider's error. The page a browser is handed is decided
// entirely by which branch of serveCallback produced it.
var (
	pageSignedIn = callbackPage{
		status: http.StatusOK,
		icon:   iconCheck,
		light:  "#1E7A3C",
		dark:   "#4FB871",
		title:  "You are signed in",
		body:   "The terminal has what it needs.",
		hint:   "You can close this tab.",
	}
	pageRefused = callbackPage{
		status: http.StatusOK,
		icon:   iconCross,
		light:  "#C0261A",
		dark:   "#E08A80",
		title:  "Login failed",
		body:   "The identity provider refused this login.",
		hint:   "Run <code>wso2 login</code> again in the terminal.",
	}
	pageStrayTab = callbackPage{
		status: http.StatusBadRequest,
		icon:   iconAlert,
		light:  "#8A6A20",
		dark:   "#D6B15C",
		title:  "This tab is not part of a login",
		body: "It did not come from a login started in this terminal, " +
			"so nothing was authorized.",
		hint: "You can close this tab.",
	}
	pageNoCode = callbackPage{
		status: http.StatusBadRequest,
		icon:   iconMinus,
		light:  "#8A6A20",
		dark:   "#D6B15C",
		title:  "Nothing to finish here",
		body:   "This address carries no authorization code.",
		hint:   "You can close this tab.",
	}
)

// The status icons, as the inner shapes of a 24x24 stroked SVG. They are drawn
// rather than lettered so each page stays one self-contained document: the
// listener answers on a loopback address that may have no network behind it,
// so nothing here may be fetched from anywhere.
const (
	iconCheck = `<circle cx="12" cy="12" r="9"/><path d="M8.5 12.4l2.4 2.4 4.6-5"/>`
	iconCross = `<circle cx="12" cy="12" r="9"/><path d="M9.2 9.2l5.6 5.6M14.8 9.2l-5.6 5.6"/>`
	iconAlert = `<path d="M12 4.5l8 14.5H4z"/><path d="M12 10v4"/><path d="M12 16.6v.1"/>`
	iconMinus = `<circle cx="12" cy="12" r="9"/><path d="M8.5 12h7"/>`
)

// callbackPage is one answer the loopback listener gives a browser: the status
// it is served with, and everything the document says.
type callbackPage struct {
	status int
	icon   string
	// light and dark are the icon's stroke in each color scheme, chosen so the
	// state reads on either. Everything else on the page shares one palette.
	light string
	dark  string
	title string
	body  string
	hint  string
}

// callbackDocument is the whole page, with the color scheme, the status color,
// the icon and the words filled in. It carries no script: a browser refuses to
// close a tab a script did not open, so the page asks rather than promises.
const callbackDocument = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>WSO2 CLI</title>
<style>
:root {
  color-scheme: light dark;
  --page: #FAFAFA; --card: #FFFFFF; --line: #E5E5E5;
  --ink: #111111; --muted: #5F5A57;
  --chip-bg: #FEF1ED; --chip-line: #F8C4B4;
  --status: %s;
}
@media (prefers-color-scheme: dark) {
  :root {
    --page: #0B0B0B; --card: #141414; --line: #262626;
    --ink: #F5F5F5; --muted: #A29C97;
    --chip-bg: #1C1614; --chip-line: #3A2A24;
    --status: %s;
  }
}
* { box-sizing: border-box; }
body {
  margin: 0; min-height: 100vh; padding: 32px 20px;
  display: flex; align-items: center; justify-content: center;
  background: var(--page); color: var(--ink);
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto,
    Helvetica, Arial, sans-serif;
  line-height: 1.6;
}
main {
  width: 100%%; max-width: 440px;
  display: flex; flex-direction: column; gap: 18px;
  background: var(--card); border: 1px solid var(--line);
  border-radius: 12px; padding: 32px 34px 28px;
}
.brand {
  margin: 0; font-size: 12px; font-weight: 640; letter-spacing: 0.14em;
  text-transform: uppercase; color: var(--muted);
}
.head { display: flex; align-items: center; gap: 12px; }
.head svg { flex-shrink: 0; stroke: var(--status); }
h1 {
  margin: 0; font-size: 22px; line-height: 1.25; font-weight: 660;
  letter-spacing: -0.015em;
}
p { margin: 0; }
.body { font-size: 15.5px; color: var(--muted); }
.product {
  align-self: flex-start; padding: 6px 12px; font-size: 13px;
  background: var(--chip-bg); border: 1px solid var(--chip-line);
  border-radius: 999px;
}
.product::before {
  content: ""; display: inline-block; width: 6px; height: 6px;
  margin-right: 8px; vertical-align: 1px;
  background: #F14E23; border-radius: 50%%;
}
hr { width: 100%%; height: 1px; margin: 2px 0 0; border: 0; background: var(--line); }
.hint { font-size: 13.5px; color: var(--muted); }
code { font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; font-size: 12.8px; }
</style>
</head>
<body>
<main>
<p class="brand">WSO2 CLI</p>
<div class="head">
<svg width="26" height="26" viewBox="0 0 24 24" fill="none" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">%s</svg>
<h1>%s</h1>
</div>
<p class="body">%s</p>
%s<hr>
<p class="hint">%s</p>
</main>
</body>
</html>
`

// render writes the page for one product. The product is the only value on the
// page that does not come from this file, so it is escaped: it is read from the
// context document, which the shell does not author.
func (p callbackPage) render(product string) string {
	chip := ""
	if strings.TrimSpace(product) != "" {
		chip = fmt.Sprintf("<p class=\"product\">%s</p>\n", html.EscapeString(product))
	}
	return fmt.Sprintf(callbackDocument, p.light, p.dark, p.icon, p.title, p.body, chip, p.hint)
}
