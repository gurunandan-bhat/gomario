package smtp

import (
	"crypto/tls"
	"fmt"
	"gomario/lib/config"

	gomail "gopkg.in/mail.v2"
)

func SendEmail(cfg *config.Config, to, subject, htmlBody string) error {

	m := gomail.NewMessage()
	m.SetHeader("From", fmt.Sprintf("%s <%s>", "Mario Gallery", cfg.Smtp.Username))
	m.SetHeader("To", to)
	m.SetHeader("Subject", subject)
	m.SetBody("text/html", htmlBody)

	// Settings for SMTP server
	d := gomail.NewDialer(
		cfg.Smtp.Host,
		cfg.Smtp.Port,
		cfg.Smtp.Username,
		cfg.Smtp.Password,
	)
	d.TLSConfig = &tls.Config{InsecureSkipVerify: true}

	if err := d.DialAndSend(m); err != nil {
		return err
	}

	return nil
}
