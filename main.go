// Copyright 2020 Marco Conti
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"errors"
	"github.com/caarlos0/env"
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/joho/godotenv"
	"github.com/urfave/cli/v2"
	"gopkg.in/go-playground/validator.v9"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Some constants
const (
	// Options/Argument names
	ArgAddr = "addr"
	ArgUser = "user"
	ArgPass = "pass"
	ArgMbox = "mbox"
	ArgPrt  = "printer"
	ArgDry  = "dry-run"
	ArgAllowed = "allowed"
	ArgVerbose  = "verbose"
	// Default mailbox name
	MailboxName = "INBOX"
	// For textual representations
	Separator = "-----------------------------------------------------"
)

// Command is the main action and its resources
type Command struct {
	c       *cli.Context
	cfg     *Config
	mclient *client.Client
	mbox    *imap.MailboxStatus
	TmpDir  string
	DryRun  bool
	Verbose bool
}

// Mail is a reduced/simplified mail message
type Mail struct {
	Date        time.Time
	From        string
	Subject     string
	Body        string
	Attachments []*Attachment
}

// Attachment is a downloaded email attachment
type Attachment struct {
	File string
	Name string
}

// Config is our main configuration store
type Config struct {
	IMAP *IMAPConfig
	Cups *CupsConfig
	Allowed []string `env:"ALLOWED" envSeparator:":"`
}

// IMAPConfig holds IMAP related configurations
type IMAPConfig struct {
	Addr    string `env:"IMAP_ADDR"                    validate:"required"`
	User    string `env:"IMAP_USER"                    validate:"required"`
	Pass    string `env:"IMAP_PASS"                    validate:"required"`
	Mailbox string `env:"IMAP_MBOX" envDefault:"INBOX" validate:"required"`
}

// CupsConfig holds cups related configurations
type CupsConfig struct {
	Printer string `env:"CUPS_PRINTER" validate:"required"`
}

// Error variables
var (
	ErrNoAttachment  = errors.New("no attachment")
	ErrInvalidSender = errors.New("invalid sender")
)

func main() {

	cmd := &Command{}

	app := cli.NewApp()
	app.Name = "IMAPPrint"
	app.Version = "1.0.0"
	app.Usage = "Query emails and print attachments"
	app.Before = cmd.bootstrap
	app.Action = cmd.action
	app.Flags = cmd.flags()

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

// action is used as callable for applications Action()
//goland:noinspection GoUnusedParameter
func (cmd *Command) action(c *cli.Context) error {

	defer cmd.shutdown()

	if cmd.mbox.Messages == 0 {
		cmd.logpad("No Messages", "Nothing to do")
		cmd.logpad(Separator)
		os.Exit(0)
	}

	seqset := new(imap.SeqSet)
	seqset.AddRange(uint32(1), cmd.mbox.Messages)

	mails, err := cmd.getMails(cmd.mclient, seqset, cmd.mbox.Messages)
	if err != nil {
		log.Fatal("Error getting messages:", err.Error())
	}

	attachments := cmd.getAttachments(mails)

	cmd.logpad(Separator)

	cmd.delexpunge(cmd.mclient, seqset)
	cmd.doprint(attachments)

	cmd.logpad("Status", "Done!")

	return nil
}

// bootstrap is used as callable for applications Before()
func (cmd *Command) bootstrap(c *cli.Context) error {

	var err error

	cmd.c = c
	cmd.DryRun = c.Bool(ArgDry)
	cmd.Verbose = c.Bool(ArgVerbose)

	if err := cmd.config(); err != nil {
		return cli.NewExitError(err, 1)
	}

	cmd.mclient, err = client.DialTLS(cmd.cfg.IMAP.Addr, nil)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	if err := cmd.mclient.Login(cmd.cfg.IMAP.User, cmd.cfg.IMAP.Pass); err != nil {
		_ = cmd.mclient.Close()
		return cli.NewExitError(err, 1)
	}

	cmd.mbox, err = cmd.mclient.Select(cmd.cfg.IMAP.Mailbox, false)
	if err != nil {
		_ = cmd.mclient.Close()
		_ = cmd.mclient.Logout()
		return cli.NewExitError(err, 1)
	}

	cmd.TmpDir, err = ioutil.TempDir("", "imap-print-")
	if err != nil {
		_ = cmd.mclient.Close()
		_ = cmd.mclient.Logout()
		return cli.NewExitError(err, 1)
	}

	cmd.logpad(Separator)
	cmd.logverb("IMAP Addr", cmd.cfg.IMAP.Addr)
	cmd.logverb("IMAP User", cmd.cfg.IMAP.User)
	cmd.logverb("IMAP Pass", "*****")
	cmd.logverb("Mailbox", cmd.cfg.IMAP.Mailbox)
	cmd.logverb("Printer", cmd.cfg.Cups.Printer)
	cmd.logpad("Dry-Run", cmd.DryRun)
	cmd.logverb("TmpDir", cmd.TmpDir)
	cmd.logverb("Allowed", cmd.cfg.Allowed)

	return nil
}

// getMails fetches emails via IMAP and returns array of simpified *Mail objects
func (cmd *Command) getMails(c *client.Client, seqset *imap.SeqSet, msgcount uint32) ([]*Mail, error) {

	var section imap.BodySectionName
	items := []imap.FetchItem{section.FetchItem()}

	messages := make(chan *imap.Message, msgcount)
	done := make(chan error, 1)

	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	if err := <-done; err != nil {
		return []*Mail{}, err
	}

	var mails []*Mail

	for msg := range messages {
		m, err := cmd.convert(msg, &section)
		if err != nil {
			if err == ErrInvalidSender {
				cmd.logpad("Error", err.Error())
			} else if err == ErrNoAttachment {
				cmd.logpad("Error", err.Error())
			} else {
				cmd.logpad("Error", err.Error())
			}
			continue
		}
		mails = append(mails, m)
	}

	if mails == nil {
		return []*Mail{}, nil
	}

	return mails, nil
}

// getAttachments returns array of *Attachment from given array of *Mail
func (cmd *Command) getAttachments(mails []*Mail) []*Attachment {

	var attachments []*Attachment

	for _, m := range mails {
		cmd.logmail(m)
		if !m.isValid(cmd.cfg.Allowed) {
			continue
		}
		for _, attachment := range m.Attachments {

			attachments = append(attachments, attachment)
		}
	}

	if attachments == nil {
		return []*Attachment{}
	}

	return attachments
}

// convert converts msg and section into simplified *Mail objects
func (cmd *Command) convert(msg *imap.Message, section *imap.BodySectionName) (*Mail, error) {

	r := msg.GetBody(section)
	if r == nil {
		log.Fatal("Server didn't return message body")
	}

	// Create a new mail reader
	mr, err := mail.CreateReader(r)
	if err != nil {
		log.Fatal(err)
	}

	m := &Mail{
		Date:        time.Now(),
		From:        "",
		Subject:     "",
		Body:        "",
		Attachments: []*Attachment{},
	}

	header := mr.Header

	if date, err := header.Date(); err == nil {
		m.Date = date
	}
	if from, err := header.AddressList("From"); err == nil {
		for _, f := range from {
			m.From = f.Address
			break
		}
	}
	if subject, err := header.Subject(); err == nil {
		m.Subject = subject
	}

	// Process each message's parts
	for {

		p, err := mr.NextPart()
		if err == io.EOF {
			break
		} else if err != nil {
			cmd.logpad("Read Message Part", err.Error())
			break
		}

		switch h := p.Header.(type) {

		case *mail.InlineHeader:

			// This is the message's text (can be plain-text or HTML)
			b, err := ioutil.ReadAll(p.Body)
			if err != nil {
				cmd.logpad("Read Message Text", err.Error())
				continue
			}
			m.Body = strings.TrimSpace(string(b))

		case *mail.AttachmentHeader:

			filename, _ := h.Filename()

			file, err := ioutil.TempFile(cmd.TmpDir, "*_"+filename)
			if err != nil {
				cmd.logpad("Create TempFiler", err.Error())
				continue
			}

			if _, err = io.Copy(file, p.Body); err != nil {
				cmd.logpad("Write Attachment", err.Error())
				_ = file.Close()
				continue
			}

			_ = file.Close()

			m.Attachments = append(
				m.Attachments,
				&Attachment{
					File: file.Name(),
					Name: filename,
				},
			)

		default:
			cmd.logpad("Unhandled Header", h)

		}

	}

	return m, nil
}

// delexpunge flags read emails as deleted and expunges
func (cmd *Command) delexpunge(c *client.Client, seqset *imap.SeqSet) {

	cmd.logverb("Cleanup", "Deleting email(s)")

	if cmd.DryRun {
		return
	}

	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.DeletedFlag}

	if err := c.Store(seqset, item, flags, nil); err != nil {
		cmd.logverb("IMAP Store Error", err.Error())
	} else {
		if err := c.Expunge(nil); err != nil {
			cmd.logpad("IMAP Expunge Error", err.Error())
		}
	}
}

// doprint loops through attachments and triggers the print
func (cmd *Command) doprint(attachments []*Attachment) {
	if attachments == nil {
		cmd.logpad("Printing", "Nothing to do")
		return
	}
	for _, attachment := range attachments {

		cmd.logverb("Printing", attachment.File)

		if cmd.DryRun {
			cmd.logpad("Printing", "OK; request id is", cmd.cfg.Cups.Printer + "-123456")
			continue
		}

		var out bytes.Buffer

		cmdExec := exec.Command("lp", "-d", cmd.cfg.Cups.Printer, attachment.File)
		cmdExec.Stdout = &out

		if err := cmdExec.Run(); err != nil {
			cmd.logverb("Printing", err.Error())
		}

		cmd.logverb("Printing", "OK; " + strings.TrimSpace(string(out.Bytes())))
	}
}

// config returns loaded *Config
func (cmd *Command) config() error {

	var err error

	if _, err = os.Stat(".env"); err == nil {
		if err = godotenv.Load(); err != nil {
			return err
		}
	}

	cmd.cfg = &Config{
		IMAP: &IMAPConfig{},
		Cups: &CupsConfig{},
		Allowed: []string{},
	}

	if err = env.Parse(cmd.cfg); err != nil {
		return err
	}

	cmd.setarg(ArgAddr)
	cmd.setarg(ArgUser)
	cmd.setarg(ArgPrt)
	cmd.setarg(ArgMbox)
	cmd.setarg(ArgPrt)
	cmd.setarg(ArgAllowed)

	validate := validator.New()
	err = validate.Struct(cmd.cfg)
	if err != nil {
		return err
	}

	return nil
}

// setarg fetches current command flags from cli context and overwrites settings where applicable
func (cmd *Command) setarg(name string) {

	v := strings.TrimSpace(cmd.c.String(name))
	if v == "" {
		return
	}

	switch true {
	case name == ArgAddr && v != "":
		cmd.cfg.IMAP.Addr = v
	case name == ArgUser && v != "":
		cmd.cfg.IMAP.User = v
	case name == ArgPass && v != "":
		cmd.cfg.IMAP.Pass = v
	case name == ArgMbox && v != "" && v != MailboxName:
		cmd.cfg.IMAP.Mailbox = v
	case name == ArgPrt && v != "":
		cmd.cfg.Cups.Printer = v
	case name == ArgAllowed && v != "":
		if strings.Contains(v, ":") {
			cmd.cfg.Allowed = []string{}
			parts := strings.Split(v, ":")
			for _, part := range parts {
				if strings.TrimSpace(part) != "" {
					cmd.cfg.Allowed = append(cmd.cfg.Allowed, strings.TrimSpace(part))
				}
			}
			return
		}
		cmd.cfg.Allowed = []string{v}
	}
}

// flags retutns current command flags
func (cmd *Command) flags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:     ArgAddr,
			Aliases:  []string{"a"},
			Usage:    "The IMAP server address `HOST:PORT`",
			Required: false,
		},
		&cli.StringFlag{
			Name:     ArgUser,
			Aliases:  []string{"u"},
			Usage:    "The IMAP account `USER`",
			Required: false,
		},
		&cli.StringFlag{
			Name:     ArgPass,
			Aliases:  []string{"p"},
			Usage:    "The IMAP account `PASS`",
			Required: false,
		},
		&cli.StringFlag{
			Name:     ArgMbox,
			Aliases:  []string{"m"},
			Usage:    "The mailbox `NAME`",
			Required: false,
			Value: MailboxName,
		},
		&cli.StringFlag{
			Name:     ArgPrt,
			Aliases:  []string{"prt"},
			Usage:    "The cups `PRINTER` name",
			Required: false,
		},
		&cli.StringFlag{
			Name:     ArgAllowed,
			Usage:    "List of allowed sender email `ADRESSES` seperated by \":\"",
			Required: false,
		},
		&cli.BoolFlag{
			Name:     ArgDry,
			Aliases:  []string{"d"},
			Usage:    "Execute a dry-run",
			Required: false,
		},
		&cli.BoolFlag{
			Name:     ArgVerbose,
			Aliases:  []string{"vv"},
			Usage:    "Verbose output",
			Required: false,
		},
	}
}

// shutdown is used to defer resources
func (cmd *Command) shutdown() {
	_ = cmd.mclient.Logout()
	_ = cmd.mclient.Close()
	if cmd.TmpDir != "" && cmd.TmpDir != os.TempDir() {
		_ = os.RemoveAll(cmd.TmpDir)
	}

}

// logmail prints out *Mail related details
func (cmd *Command) logmail(m *Mail) {
	cmd.logpad(Separator)
	cmd.logpad("Date", m.Date)
	cmd.logpad("From", m.From)
	cmd.logpad("Subject", m.Subject)
	cmd.logpad("Text", m.Body)
	cmd.logpad("Attachments", len(m.Attachments))
	cmd.logpad("ValidSender", m.isValidSender(cmd.cfg.Allowed))
	cmd.logpad("HasAttachments", m.hasAttachments())
	if m.isValid(cmd.cfg.Allowed) {
		cmd.logpad("Status", "Ok!")
	} else {
		cmd.logpad("Status", "Will be ignored...")
	}
}

// logpad prints out a predefined key-value output
func (cmd *Command) logpad(title string, v ...interface{}) {

	t := strings.TrimSpace(title)

	if v == nil || len(v) == 0 {
		log.Println(t)
		return
	}

	if !strings.HasSuffix(t, ":") {
		t += ": "
	}

	if len(t) < 17 {
		t += strings.Repeat(" ", 17 - len(t))
	}

	var items []interface{}

	items = append(items, t)

	for _, item := range v {
		items = append(items, item)
	}

	log.Println(items...)
}

// logverb prints out a predefined key-value output if run in verbose
func (cmd *Command) logverb(title string, v ...interface{}) {
	if cmd.Verbose {
		cmd.logpad(title, v...)
	}
}


// isValid checks if mail is valid for printing
func (m *Mail) isValid(allowed []string) bool {
	return m.hasAttachments() && m.isValidSender(allowed)
}

// hasAttachments checks if *Mail has attachments
func (m *Mail) hasAttachments() bool {
	return len(m.Attachments) > 0
}

// isValidSender checks if *Mail has a valid sender
func (m *Mail) isValidSender(allowed []string) bool {
	for _, item := range allowed {
		if item == m.From {
			return true
		}
	}
	return false
}
