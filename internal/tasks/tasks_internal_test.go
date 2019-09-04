package tasks

import (
	"bytes"
	"encoding/base64"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/gomail.v2"
)

var now = time.Now

func init() {
	now = func() time.Time {
		return time.Date(2014, 06, 25, 17, 46, 0, 0, time.UTC)
	}
}

type message struct {
	from    string
	to      []string
	content string
}

var boundaryRegExp = regexp.MustCompile("boundary=(\\w+)")

func TestDownloadFile(t *testing.T) {
	downloadUrls = func(urls []string, dest string, workers int) error { return nil }
	assert.EqualError(t, taskDownloadFile(""), `unexpected end of JSON input`,
		"Download with empty param should fail")
	assert.EqualError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": [] }`),
		"Files to download are not specified", "Download with empty files should fail")
	assert.Error(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "non-existent" }`),
		"Downlod with non-existent directory or insufficient rights should fail")
	assert.NoError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "." }`),
		"Downlod with correct json input should succeed")
}

func TestMessage(t *testing.T) {
	m := gomail.NewMessage()
	m.SetHeader("From", "from@example.com")
	m.SetHeader("To", "to@example.com")
	m.SetHeader("Cc", "cc@example.com")
	m.SetHeader("Bcc", "bcc@example.com")
	m.SetHeader("Subject", "Hello Pg_Timetable")
	m.SetDateHeader("X-Date", now())
	m.SetHeader("X-Date-2", m.FormatDate(now()))
	m.SetBody("text/plain", "Hello, This mail from pg_timetable!!")

	want := &message{
		from: "from@example.com",
		to: []string{
			"to@example.com",
			"cc@example.com",
			"bcc@example.com",
		},
		content: "Subject: Hello Pg_Timetable\r\n" +
			"From: from@example.com\r\n" +
			"To: to@example.com\r\n" +
			"Cc: cc@example.com\r\n" +
			"X-Date: Wed, 25 Jun 2014 17:46:00 +0000\r\n" +
			"X-Date-2: Wed, 25 Jun 2014 17:46:00 +0000\r\n" +
			"Content-Transfer-Encoding: quoted-printable\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" +
			"Hello, This mail from pg_timetable!!",
	}

	testMail(t, m, 0, want)
}

func TestRecipients(t *testing.T) {
	m := gomail.NewMessage()
	m.SetHeaders(map[string][]string{
		"From":    {"from@example.com"},
		"To":      {"to@example.com"},
		"Cc":      {"cc@example.com"},
		"Bcc":     {"bcc1@example.com", "bcc2@example.com"},
		"Subject": {"Hello, This mail from pg_timetable!!"},
	})
	m.SetBody("text/plain", "Test message")

	want := &message{
		from: "from@example.com",
		to:   []string{"to@example.com", "cc@example.com", "bcc1@example.com", "bcc2@example.com"},
		content: "From: from@example.com\r\n" +
			"To: to@example.com\r\n" +
			"Cc: cc@example.com\r\n" +
			"Subject: Hello, This mail from pg_timetable!!\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"Content-Transfer-Encoding: quoted-printable\r\n" +
			"\r\n" +
			"Test message",
	}

	testMail(t, m, 0, want)
}

func TestAttachments(t *testing.T) {
	m := gomail.NewMessage()
	m.SetHeader("From", "from@example.com")
	m.SetHeader("To", "to@example.com")
	m.SetBody("text/plain", "Test")
	m.Attach(mockCopyFile("/tmp/test.pdf"))
	m.Attach(mockCopyFile("/tmp/test.zip"))

	want := &message{
		from: "from@example.com",
		to:   []string{"to@example.com"},
		content: "From: from@example.com\r\n" +
			"To: to@example.com\r\n" +
			"Content-Type: multipart/mixed;\r\n" +
			" boundary=_BOUNDARY_1_\r\n" +
			"\r\n" +
			"--_BOUNDARY_1_\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"Content-Transfer-Encoding: quoted-printable\r\n" +
			"\r\n" +
			"Test\r\n" +
			"--_BOUNDARY_1_\r\n" +
			"Content-Type: application/pdf; name=\"test.pdf\"\r\n" +
			"Content-Disposition: attachment; filename=\"test.pdf\"\r\n" +
			"Content-Transfer-Encoding: base64\r\n" +
			"\r\n" +
			base64.StdEncoding.EncodeToString([]byte("Content of test.pdf")) + "\r\n" +
			"--_BOUNDARY_1_\r\n" +
			"Content-Type: application/x-zip-compressed; name=\"test.zip\"\r\n" +
			"Content-Disposition: attachment; filename=\"test.zip\"\r\n" +
			"Content-Transfer-Encoding: base64\r\n" +
			"\r\n" +
			base64.StdEncoding.EncodeToString([]byte("Content of test.zip")) + "\r\n" +
			"--_BOUNDARY_1_--\r\n",
	}

	testMail(t, m, 1, want)

}

func testMail(t *testing.T, m *gomail.Message, bCount int, want *message) {
	err := gomail.Send(sendMail(t, bCount, want), m)
	if err != nil {
		t.Error(err)
	}
}

func sendMail(t *testing.T, bCount int, want *message) gomail.SendFunc {
	return func(from string, to []string, m io.WriterTo) error {
		if from != want.from {
			t.Fatalf("Invalid from, got %q, want %q", from, want.from)
		}

		if len(to) != len(want.to) {
			t.Fatalf("Invalid recipient count, \ngot %d: %q\nwant %d: %q",
				len(to), to,
				len(want.to), want.to,
			)
		}
		for i := range want.to {
			if to[i] != want.to[i] {
				t.Fatalf("Invalid recipient, got %q, want %q",
					to[i], want.to[i],
				)
			}
		}

		buf := new(bytes.Buffer)
		_, err := m.WriteTo(buf)
		if err != nil {
			t.Error(err)
		}
		got := buf.String()
		wantMsg := string("Mime-Version: 1.0\r\n" +
			"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
			want.content)
		if bCount > 0 {
			boundaries := getBoundaries(t, bCount, got)
			for i, b := range boundaries {
				wantMsg = strings.Replace(wantMsg, "_BOUNDARY_"+strconv.Itoa(i+1)+"_", b, -1)
			}
		}

		compareBodies(t, got, wantMsg)
		return nil
	}
}

func mockCopyFile(name string) (string, gomail.FileSetting) {
	return name, gomail.SetCopyFunc(func(w io.Writer) error {
		_, err := w.Write([]byte("Content of " + filepath.Base(name)))
		return err
	})
}

func compareBodies(t *testing.T, got, want string) {
	// We cannot do a simple comparison since the ordering of headers' fields
	// is random.
	gotLines := strings.Split(got, "\r\n")
	wantLines := strings.Split(want, "\r\n")

	// We only test for too many lines, missing lines are tested after
	if len(gotLines) > len(wantLines) {
		t.Fatalf("Message has too many lines, \ngot %d:\n%s\nwant %d:\n%s", len(gotLines), got, len(wantLines), want)
	}

	isInHeader := true
	headerStart := 0
	for i, line := range wantLines {
		if line == gotLines[i] {
			if line == "" {
				isInHeader = false
			} else if !isInHeader && len(line) > 2 && line[:2] == "--" {
				isInHeader = true
				headerStart = i + 1
			}
			continue
		}

		if !isInHeader {
			missingLine(t, line, got, want)
		}

		isMissing := true
		for j := headerStart; j < len(gotLines); j++ {
			if gotLines[j] == "" {
				break
			}
			if gotLines[j] == line {
				isMissing = false
				break
			}
		}
		if isMissing {
			missingLine(t, line, got, want)
		}
	}
}

func missingLine(t *testing.T, line, got, want string) {
	t.Fatalf("Missing line %q\ngot:\n%s\nwant:\n%s", line, got, want)
}

func getBoundaries(t *testing.T, count int, m string) []string {
	if matches := boundaryRegExp.FindAllStringSubmatch(m, count); matches != nil {
		boundaries := make([]string, count)
		for i, match := range matches {
			boundaries[i] = match[1]
		}
		return boundaries
	}

	t.Fatal("Boundary not found in body")
	return []string{""}
}
