package main

import (
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
)

func SendAlertEmail(cfg *Config, problems []*ServiceState) error {
	subj := fmt.Sprintf("%s — %d service(s) need attention", cfg.Mail.Subject, len(problems))
	return sendMail(cfg, subj, buildAlertHTML(problems))
}

func SendRecoveryEmail(cfg *Config, recovered []*ServiceState) error {
	subj := fmt.Sprintf("%s — %d service(s) recovered", cfg.Mail.Subject, len(recovered))
	return sendMail(cfg, subj, buildRecoveryHTML(recovered))
}

// sendMail uses a plain SMTP conversation with no AUTH and no STARTTLS,
// matching the Java sample (Session.getInstance(props) without authenticator).
// net/smtp's SendMail helper would auto-negotiate STARTTLS if the server
// advertises it, which would fail against an internal relay with no usable
// cert — we avoid that by driving the client manually.
func sendMail(cfg *Config, subject, htmlBody string) error {
	addr := net.JoinHostPort(cfg.Mail.Host, fmt.Sprintf("%d", cfg.Mail.Port))

	c, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("smtp dial %s: %w", addr, err)
	}
	defer c.Close()

	helloName, _ := os.Hostname()
	if helloName == "" {
		helloName = "watchdog"
	}
	if err := c.Hello(helloName); err != nil {
		return fmt.Errorf("smtp hello: %w", err)
	}
	if err := c.Mail(cfg.Mail.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	for _, to := range cfg.Recipients {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("smtp rcpt %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}

	var msg strings.Builder
	msg.WriteString("From: " + cfg.Mail.From + "\r\n")
	msg.WriteString("To: " + strings.Join(cfg.Recipients, ", ") + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("Date: " + time.Now().Format(time.RFC1123Z) + "\r\n")
	msg.WriteString(fmt.Sprintf("Message-ID: <%d.watchdog@%s>\r\n", time.Now().UnixNano(), helloName))
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	if _, err := w.Write([]byte(msg.String())); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return c.Quit()
}

func buildAlertHTML(problems []*ServiceState) string {
	var b strings.Builder
	b.WriteString(htmlPrefix("Java Service Watchdog — Alert", "#b91c1c"))
	host, _ := os.Hostname()
	b.WriteString(`<p>The following Java services on <b>` + htmlEsc(host) + `</b> need attention:</p>`)
	b.WriteString(`<table cellpadding="8" cellspacing="0" style="border-collapse:collapse;border:1px solid #ccc;font-size:13px;">`)
	b.WriteString(`<tr style="background:#f3f4f6;">`)
	for _, h := range []string{"Service", "Status", "Detail", "Active Log File", "PID"} {
		b.WriteString(`<th style="border:1px solid #ccc;text-align:left;">` + h + `</th>`)
	}
	b.WriteString(`</tr>`)
	for _, st := range problems {
		color := "#f59e0b" // amber = STUCK
		switch st.CurrentStatus {
		case StatusCrashed:
			color = "#dc2626" // red
		case StatusUnknown:
			color = "#7c3aed" // purple
		}
		pid := st.PID
		if !st.ProcessAlive || pid == "" {
			pid = "NOT FOUND"
		}
		b.WriteString(`<tr>`)
		b.WriteString(`<td style="border:1px solid #ccc;"><b>` + htmlEsc(st.Config.Name) + `</b></td>`)
		b.WriteString(`<td style="border:1px solid #ccc;color:` + color + `;"><b>` + string(st.CurrentStatus) + `</b></td>`)
		b.WriteString(`<td style="border:1px solid #ccc;">` + htmlEsc(st.DetectionDetail) + `</td>`)
		b.WriteString(`<td style="border:1px solid #ccc;font-family:Consolas,monospace;font-size:12px;">` + htmlEsc(st.LastLogFile) + `</td>`)
		b.WriteString(`<td style="border:1px solid #ccc;">` + htmlEsc(pid) + `</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	b.WriteString(`<p style="color:#6b7280;font-size:12px;margin-top:8px;">This alert will repeat until the services recover. A recovery email will be sent when they do.</p>`)
	b.WriteString(htmlSuffix())
	return b.String()
}

func buildRecoveryHTML(recovered []*ServiceState) string {
	var b strings.Builder
	b.WriteString(htmlPrefix("Java Service Watchdog — Recovery", "#16a34a"))
	host, _ := os.Hostname()
	b.WriteString(`<p>The following Java services on <b>` + htmlEsc(host) + `</b> have recovered:</p>`)
	b.WriteString(`<table cellpadding="8" cellspacing="0" style="border-collapse:collapse;border:1px solid #ccc;font-size:13px;">`)
	b.WriteString(`<tr style="background:#f3f4f6;">`)
	b.WriteString(`<th style="border:1px solid #ccc;text-align:left;">Service</th>`)
	b.WriteString(`<th style="border:1px solid #ccc;text-align:left;">Detail</th></tr>`)
	for _, st := range recovered {
		b.WriteString(`<tr>`)
		b.WriteString(`<td style="border:1px solid #ccc;"><b>` + htmlEsc(st.Config.Name) + `</b></td>`)
		b.WriteString(`<td style="border:1px solid #ccc;">` + htmlEsc(st.DetectionDetail) + `</td>`)
		b.WriteString(`</tr>`)
	}
	b.WriteString(`</table>`)
	b.WriteString(htmlSuffix())
	return b.String()
}

func htmlPrefix(title, color string) string {
	return `<!DOCTYPE html><html><head><meta charset="UTF-8"></head>` +
		`<body style="font-family:Arial,Helvetica,sans-serif;">` +
		`<h2 style="color:` + color + `;margin:0 0 12px 0;">` + title + `</h2>`
}

func htmlSuffix() string {
	return `<p style="color:#6b7280;font-size:12px;margin-top:16px;">Sent at ` +
		time.Now().Format("2006-01-02 15:04:05 -0700") +
		` by Java Services Watchdog.</p></body></html>`
}

func htmlEsc(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		`'`, "&#39;",
	).Replace(s)
}