# IMAP-Print

Application connects to IMAP based mail-server via TLS, fetches available emails, downloads corresponding attachments
and finaly sends them to a configured printer via cups. A cronjob on my home server execs application every minute and
checks if there is smomething to do.

## Run & Build

```bash
go get ./...
go build -o imap-print *.go
```

## Prerequisites

You have to set up cups on your machine with a configured printer which we can talk to by name.

## Configuration

You can use environment variables to configure IMAP-Printer. In addition, you can place a .env file in your current
working directory.

```
IMAP_ADDR=mail.example.com:993
IMAP_USER=myprinter@example.com
IMAP_PASS=mypassword
IMAP_MBOX=INBOX
CUPS_PRINTER=Officejet-6000-E609a
ALLOWED=marco@example.com:someone@somewhere.com
```

## Application Options

If you do not want to use a .env file you can also make use of direct application options:

```
NAME:
   IMAPPrint - Query emails and print attachments

USAGE:
   imap-print [global options] command [command options] [arguments...]

VERSION:
   1.0.0

COMMANDS:
   help, h  Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --addr HOST:PORT, -a HOST:PORT    The IMAP server address HOST:PORT
   --user USER, -u USER              The IMAP account USER
   --pass PASS, -p PASS              The IMAP account PASS
   --mbox NAME, -m NAME              The mailbox NAME (default: "INBOX")
   --printer PRINTER, --prt PRINTER  The cups PRINTER name
   --allowed ADRESSES                List of allowed sender email ADRESSES seperated by ":"
   --dry-run, -d                     Execute a dry-run (default: false)
   --verbose, --vv                   Verbose output (default: false)
   --help, -h                        show help (default: false)
   --version, -v                     print the version (default: false)

```

In addition to only configure each run, you can also trigger a dry-run to test what is going on. A more verbose mode is
also available.

## Cups Bash

To get an idea of available/usable cups CLI calls:

```bash
prtname=Officejet-6000-E609a

# All printers
lpstat -p -d

# Printer details
lpoptions -p ${prtname}

# Print file
lp -d ${prtname} ~/Documents/sample.pdf
```

## TODO

 * Replace exec.Command() calls by original cups api calls via C headers
 * Implement restriction to a set of printable document types (extensions)
 * Implement response email with results
