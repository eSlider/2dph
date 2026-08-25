// Package oohtml builds the branded produktor.io HTML mail template and runs
// the render-check gate that blocks sending until the draft round-trips with
// paragraphs, the cid-embedded logo and the signature intact.
//
// The palette is a brand constant from produktor.io (issue #76); the logo is
// referenced by Content-ID (cid:) so mail clients render it without loading a
// remote image. Inline style= attributes only — email clients strip <style>.
package oohtml

import (
	"fmt"
	"strings"
)

// produktor.io brand palette (extracted from the site CSS bundle, #76).
const (
	PaletteCream      = "#FAF5EA" // page background / offer card
	PaletteInk        = "#0A0A0A" // near-black text
	PaletteNavy       = "#142039" // dark footer
	PaletteNavyDeep   = "#0E1626" // darkest footer
	PaletteCTA        = "#F2C849" // yellow CTA
	PalettePrimary    = "#143A6F" // primary blue
	PalettePrimaryLt  = "#5389CE" // lighter blue
	PalettePastelBlue = "#B6CDEC" // pastel blue
	// PaletteRed #C1372C is reserved for errors only — never in mail.
)

const (
	// LogoCID is the Content-ID of the embedded logo. The template references
	// it via <img src="cid:...">; the client resolves it against the attached
	// file whose filename matches (no remote-image loading).
	LogoCID = "produktor-logo.gif"
	// LogoFilename is the attachment filename / Content-ID stem.
	LogoFilename = "produktor-logo.gif"
	// LogoContentType is the GIF MIME type.
	LogoContentType = "image/gif"
	// LogoPath is the committed email-friendly logo asset, relative to the
	// repo root (no absolute paths in committed code).
	LogoPath = "etc/onlyoffice/assets/produktor-logo.gif"
)

// TemplateData is the typed input for the branded mail shell. BodyHTML holds
// the already-HTML body paragraphs; for plain-text input wrap it with
// onlyoffice.PlainTextToMailHTML first (see Mailer.Send).
type TemplateData struct {
	Greeting     string // first line, e.g. "Hello Alice,"
	BodyHTML     string // body paragraphs as <p>…</p> blocks
	OfferHeading string // offer-block heading
	OfferText    string // offer-block copy
	CTAURL       string // offer call-to-action link
	CTALabel     string // offer call-to-action label
	DemoURL      string // live WebDAV demo-drive link (issue #72)
	DemoLabel    string // demo-drive link label
	SignerName   string // signature name / initials
	SignerRole   string // signature role
	SiteURL      string // produktor.io link target
	SiteLabel    string // produktor.io link label
}

// Build renders the branded HTML mail with the cid-embedded logo, greeting,
// body, offer block and signature. Errors on missing required fields.
func Build(d TemplateData) (string, error) {
	if strings.TrimSpace(d.Greeting) == "" {
		return "", fmt.Errorf("oohtml: greeting is required")
	}
	if strings.TrimSpace(d.OfferHeading) == "" {
		return "", fmt.Errorf("oohtml: offer heading is required")
	}
	if strings.TrimSpace(d.SignerName) == "" {
		return "", fmt.Errorf("oohtml: signer name is required")
	}
	body := strings.TrimSpace(d.BodyHTML)
	if body == "" {
		return "", fmt.Errorf("oohtml: body is required")
	}

	var b strings.Builder
	b.Grow(2048)
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"/><meta name="viewport" content="width=device-width"/></head>
<body style="margin:0;padding:0;background-color:` + PaletteCream + `;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:` + PaletteCream + `;">
<tr><td align="center" style="padding:24px 16px;">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="width:100%;max-width:600px;background-color:#ffffff;border-radius:12px;overflow:hidden;border:1px solid ` + PalettePastelBlue + `;">

	<!-- header -->
	<tr><td style="background-color:` + PalettePrimary + `;padding:20px 28px;">
		<img src="cid:` + LogoCID + `" alt="produktor.io" width="64" height="64" style="display:block;width:64px;height:64px;border-radius:8px;"/>
	</td></tr>

	<!-- body -->
	<tr><td style="padding:28px;font-family:system-ui,-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;font-size:16px;line-height:1.6;color:` + PaletteInk + `;">
		<p style="margin:0 0 16px;">` + esc(d.Greeting) + `</p>
		` + body + `
	</td></tr>

	<!-- offer block -->
	<tr><td style="padding:0 28px 28px;">
		<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background-color:` + PaletteCream + `;border-radius:10px;border:1px solid ` + PalettePastelBlue + `;">
		<tr><td style="padding:20px 24px;font-family:system-ui,Arial,sans-serif;">
			<p style="margin:0 0 6px;font-size:15px;font-weight:600;color:` + PaletteNavy + `;">` + esc(d.OfferHeading) + `</p>
			<p style="margin:0 0 16px;font-size:15px;color:` + PaletteInk + `;">` + esc(d.OfferText) + `</p>
			<table role="presentation" cellpadding="0" cellspacing="0"><tr><td style="border-radius:6px;background-color:` + PaletteCTA + `;">
				<a href="` + esc(d.CTAURL) + `" style="display:inline-block;padding:10px 22px;font-family:system-ui,Arial,sans-serif;font-size:15px;font-weight:600;color:` + PaletteNavy + `;text-decoration:none;border-radius:6px;">` + esc(d.CTALabel) + `</a>
			</td></tr></table>
			` + demoLink(d) + `
		</td></tr></table>
	</td></tr>

	<!-- signature -->
	<tr><td style="padding:0 28px 24px;font-family:system-ui,Arial,sans-serif;font-size:15px;color:` + PaletteInk + `;">
		<p style="margin:0;">Best regards,</p>
		<p style="margin:4px 0 0;font-weight:600;color:` + PaletteNavy + `;">` + esc(d.SignerName) + `</p>
		<p style="margin:0;color:` + PalettePrimary + `;">` + esc(d.SignerRole) + `</p>
		<p style="margin:2px 0 0;"><a href="` + esc(d.SiteURL) + `" style="color:` + PalettePrimary + `;text-decoration:none;">` + esc(d.SiteLabel) + `</a></p>
	</td></tr>

	<!-- footer -->
	<tr><td style="background-color:` + PaletteNavyDeep + `;padding:16px 28px;font-family:system-ui,Arial,sans-serif;font-size:12px;color:` + PalettePastelBlue + `;">
		<p style="margin:0;">produktor.io — product engineering.</p>
	</td></tr>

</table>
</td></tr>
</table>
</body>
</html>
`)
	return b.String(), nil
}

// esc escapes a string for safe inline HTML attribute/text usage.
func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;")
	return r.Replace(s)
}

// demoLink renders the secondary live-demo-drive link under the offer CTA when
// DemoURL is set (issue #72). Empty DemoURL yields no extra markup.
func demoLink(d TemplateData) string {
	if strings.TrimSpace(d.DemoURL) == "" {
		return ""
	}
	label := strings.TrimSpace(d.DemoLabel)
	if label == "" {
		label = "Try the live WebDAV drive"
	}
	return `<p style="margin:10px 0 0;font-size:14px;"><a href="` + esc(d.DemoURL) + `" style="color:` + PalettePrimary + `;text-decoration:underline;">` + esc(label) + `</a></p>`
}
