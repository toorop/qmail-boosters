/*

   Copyright 2013 Stéphane Depierrepont

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.

	SYNOPSIS
          qmail-remote host sender recip [ recip ... ]

    More details : http://www.qmail.org/man/man8/qmail-remote.html


    Config files (/var/qmail/control)
		Line begining with "#" or " " are ignored (comments)

    	routes :
		# comments
		# name:host:port:username:passwd
		mailjet:in.mailjet.com:25:username:password
		pm:mx5.protecmail.com:25::

    	routemap
		#senderHost:recipientHost:routeName
		*:protecmail.com:pm
		*:toorop.fr:pm
		*:ovh.com:mx1.ovh.net
		*:*:mailjet
*/

package main

import (
	"bufio"
	"bytes"
	//"crypto/tls"
	"flag"
	"fmt"
	"github.com/Toorop/qmail-boosters/src/smtp"
	"io/ioutil"
	"net"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	ZEROBYTE byte = 0
)

type Route struct {
	name     string
	host     string
	dsnHost  string // IP or Hostname
	port     string
	username string
	passwd   string
}

type SmtpResponse struct {
	code int
	msg  string
}

var (
	sender     string
	recipients []string
	qbUuid     string
)

func zero() {
	zb := make([]byte, 1)
	zb[0] = ZEROBYTE
	os.Stdout.Write(zb)
	os.Stdout.Sync()
}

func zerodie() {
	zero()
	os.Exit(0)
}

func out(msg string) {
	fmt.Print(fmt.Sprintf("%s", msg))
}

func dieUsage() {
	fmt.Print("DI (qmail-remote) was invoked improperly. (#5.3.5)\n")
	zerodie()
}

func dieRead() {
	fmt.Printf("Z%s:%s:%s:Unable to read message. (#4.3.0)\n", qbUuid, sender, strings.Join(recipients, ","))
	zerodie()
}

func dieControl() {
	fmt.Printf("Z%s:%s:%s:Unable to read control files. (#4.3.0)\n", qbUuid, sender, strings.Join(recipients, ","))
	zerodie()
}

func dieControlRoutes() {
	fmt.Printf("Z%s:%s:%s:Unable to read control file 'routes'. Bad format or file not found (#4.3.0)\n", qbUuid, sender, strings.Join(recipients, ","))
	zerodie()
}

func dieControlRoutesDefaultAsNameIsForbidden() {
	fmt.Printf("Z%s:%s:%s:Name 'default' for a route is forbidden (#4.3.0)\n", qbUuid, sender, strings.Join(recipients, ","))
	zerodie()
}

func dieBadRcptTo() {
	fmt.Print("ZUnable to parse recipients. (#4.3.0)\n")
	zerodie()
}

func dieBadMailFrom() {
	fmt.Print("ZUnable to parse sender. (#4.3.0)\n")
	zerodie()
}

func dieRouteNotFound(route string) {
	fmt.Printf("ZRoute '%s' not found in control/routes. (#4.3.0)\n", route)
	zerodie()
}

func dieBadSmtpResponse(response string) {
	fmt.Printf("ZSorry but i don't understand SMTP response : %s \n", response)
	zerodie()
}

func tempNoCon(dsn string) {
	fmt.Printf("Z%s:%s:%s:Sorry, I wasn't able to establish an SMTP connection to %s. (#4.4.1)\n", qbUuid, sender, strings.Join(recipients, ","), dsn)
	zerodie()
}

func tempTimeout(dsn string) {
	fmt.Printf("Z%s:%s:%s:Sorry, timeout occured while speaking to %s. (#4.4.1)\n", qbUuid, sender, strings.Join(recipients, ","), dsn)
	zerodie()
}

func tempTlsFailed(dsn string) {
	fmt.Printf("Z%s:%s:%s:Remote host %s accept STARTTLS but init TLS failed (#4.4.1)\n", qbUuid, sender, strings.Join(recipients, ","), dsn)
	zerodie()
}

func permNoMx(host string) {
	fmt.Printf("D%s:%s:%s:Sorry, I couldn't find a mail exchanger or IP address for host %s. (#5.4.4)\n", qbUuid, sender, strings.Join(recipients, ","), host)
	zerodie()
}

func permDebug(msg string) {
	fmt.Printf("D%s\n", msg)
	zerodie()
}

func tempAuthFailure(host, msg string) {
	fmt.Printf("Z%s:%s:%s:Auth failure (perhaps temp) dialing to host %s -> %s (#5.4.4)\n", qbUuid, sender, strings.Join(recipients, ","), host, msg)
	zerodie()
}

func timeout(timeout chan bool, remain int) {
	time.Sleep(time.Duration(remain) * time.Second)
	timeout <- true
}

func doTimeout(timeout chan bool, dsn string) {
	s := <-timeout
	if s == true {
		tempTimeout(dsn)
	}
}

func newSmtpResponse(resp string) (smtpResponse SmtpResponse) {
	var err error
	t := strings.Split(resp, " ")
	smtpResponse.code, err = strconv.Atoi(t[0])
	if err != nil {
		dieBadSmtpResponse(resp)
	}
	smtpResponse.msg = strings.Join(t[1:], " ")
	return
}

func readControl(ctrlFile string) (lines []string, err error) {
	//file := fmt.Sprintf("../testutils/%s", ctrlFile) // debugging purpose
	file := fmt.Sprintf("/var/qmail/%s", ctrlFile)
	f, err := os.Open(file)
	if err != nil {
		return
		//dieControl()
	}
	r := bufio.NewReader(f)
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			break
		}
		if line[0] == 35 || line[0] == 32 {
			continue
		}
		if len(line) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s", line))
	}
	return
}

func getRoutes() map[string]Route {
	var routes = map[string]Route{}
	routesC, err := readControl("control/routes")

	if err == nil {
		for _, route := range routesC {
			parsed := strings.Split(route, ":")
			if len(parsed) != 5 {
				dieControlRoutes()
			}
			if parsed[0] == "default" {
				dieControlRoutesDefaultAsNameIsForbidden()
			}
			var r Route
			r.name = parsed[0]
			r.host = parsed[1]
			r.port = parsed[2]
			r.username = parsed[3]
			r.passwd = parsed[4]
			routes[r.name] = r
		}
		// default
		var d Route
		d.name = "default"
		routes["default"] = d
	}
	return routes
}

func getHeloHost() (heloHost string) {
	t, err := readControl("control/me")
	if err != nil {
		dieControl()
	}
	heloHost = strings.Trim(t[0], " ")
	return
}

func getRoute(sender string, remoteHost string) (route Route) {
	var senderHost string

	route.name = "default"

	// if remotehost is an IP skip test
	if net.ParseIP(remoteHost) != nil {
		route.name = remoteHost
		route.host = remoteHost
		route.port = "25"
		return
	}

	t := strings.Split(sender, "@")
	if len(t) == 1 { // bounce
		senderHost = "bounce"
	} else {
		senderHost = t[1]
	}

	routes := getRoutes()
	if len(routes) > 0 {
		routesMap, err := readControl("control/routemap")
		if err != nil {
			dieControl()
		}
		for _, r1 := range routesMap {
			parsed := strings.Split(r1, ":")
			if (parsed[0] == "*" || parsed[0] == senderHost) && (parsed[1] == "*" || parsed[1] == remoteHost) {
				route = routes[parsed[2]]
				break
			}
		}
		if route.name != "default" {
			return
		}
	}

	// try to find route in smtproutes
	smtproutes, err := readControl("control/smtproutes")
	if err == nil {
		for _, l := range smtproutes {
			s := strings.Split(l, ":")
			if s[0] == remoteHost {
				route.name = "smtproutes"
				route.host = s[1]
				route.port = "25"
				break
			}
		}
	}
	return
}

func sendmail(remoteHost string, sender string, recipients []string, data *string, route Route) {

	// @todo : try secondary MX if primary failed

	// Extract qmail-booster UUID from header (need qmail-booster version of qmail-smtpd (coming soon))
	qbUuid = "" // default
	bufh := bytes.NewBufferString(*data)
	mailmsg, e := mail.ReadMessage(bufh)
	if e == nil {
		qbUuid = mailmsg.Header.Get("X-QB-UUID")
	}

	// helloHost
	helloHost := getHeloHost()

	if route.name == "default" {
		// get MX
		mxs, err := net.LookupMX(remoteHost)
		// No MX -> use A (remoteHost)
		if err != nil {
			route.host = remoteHost
		} else {
			route.host = mxs[0].Host[0 : len(mxs[0].Host)-1]
		}
		route.port = "25"
	}

	if route.port == "" {
		route.port = "25"
	}

	// if hostname get IP
	route.dsnHost = route.host
	if net.ParseIP(route.host) == nil {
		t, err := net.LookupHost(route.host)
		if err != nil {
			permNoMx(remoteHost)
		} else {
			route.dsnHost = t[0]
		}
	}

	dsn := fmt.Sprintf("%s:%s", route.dsnHost, route.port)

	// Timeout 60 seconds
	timeoutCon := make(chan bool, 1)
	go timeout(timeoutCon, 240)
	go doTimeout(timeoutCon, dsn)

	// Connect
	c, err := smtp.Dial(dsn, helloHost)
	if err != nil {
		tempNoCon(dsn)
	}
	defer c.Quit()

	// STARTTLS ?
	// 2013-06-22 14:19:30.670252500 delivery 196893: deferral: Sorry_but_i_don't_understand_SMTP_response_:_local_error:_unexpected_message_/
	// 2013-06-18 10:08:29.273083500 delivery 856840: deferral: Sorry_but_i_don't_understand_SMTP_response_:_failed_to_parse_certificate_from_server:_negative_serial_number_/
	// https://code.google.com/p/go/issues/detail?id=3930
	/*if ok, _ := c.Extension("STARTTLS"); ok {
		var config tls.Config
		config.InsecureSkipVerify = true
		// If TLS nego failed bypass secure transmission
		_ = c.StartTLS(&config)
	}

	// Auth
	var auth smtp.Auth
	if route.username != "" && route.passwd != "" {
		_, auths := c.Extension("AUTH")

		if strings.Contains(auths, "CRAM-MD5") {
			auth = smtp.CRAMMD5Auth(route.username, route.passwd)
		} else { // PLAIN
			auth = smtp.PlainAuth("", route.username, route.passwd, route.host)
		}
	}

	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			err := c.Auth(auth)
			if err != nil {
				msg := fmt.Sprintf("%s", err)
				tempAuthFailure(dsn, msg)
			}
		}
	}*/

	if err := c.Mail(sender); err != nil {
		c.Quit()
		smtpR := newSmtpResponse(err.Error())
		if smtpR.code >= 500 {
			out("D")
		} else {
			out("Z")
		}
		out(fmt.Sprintf("%s:%s:%s:Connected to %s but sender was rejected. %s.\n", qbUuid, sender, strings.Join(recipients, ","), dsn, smtpR.msg))
		zerodie()
	}

	flagAtLeastOneRecipitentSuccess := false
	for _, rcptto := range recipients {
		if err := c.Rcpt(rcptto); err != nil {
			smtpR := newSmtpResponse(err.Error())
			if smtpR.code >= 500 {
				out("h")
			} else { // code >=400
				out("s")
			}
			out(fmt.Sprintf("%s:%s:%s:%s:", qbUuid, sender, rcptto, dsn))
			out(" does not like recipient.")
			out(smtpR.msg)
		} else {
			out("r")
			out(fmt.Sprintf("%s:%s:%s:%s:recipient accepted.", qbUuid, sender, rcptto, dsn))
			flagAtLeastOneRecipitentSuccess = true
		}
		zero()
	}

	if !flagAtLeastOneRecipitentSuccess {
		out("D")
		//out(fmt.Sprintf("%s:%s:%s:", sender, strings.Join(recipients, ","), dsn))
		out("Giving up on ")
		out(route.dsnHost)
		out("\n")
		zerodie()
	}

	w, err := c.Data()
	if err != nil {
		smtpR := newSmtpResponse(err.Error())
		if smtpR.code >= 500 {
			out("D")
		} else { // code >=400
			out("Z")
		}
		//out(fmt.Sprintf("%s:%s:%s:", sender, strings.Join(recipients, ","), dsn))
		out(route.host)
		out(" failed on DATA command : ")
		out(smtpR.msg)
		out("\n")
		zerodie()
	}

	buf := bytes.NewBufferString(*data)
	if _, err := buf.WriteTo(w); err != nil {
		out("Z")
		//out(fmt.Sprintf("%s:%s:%s:", sender, strings.Join(recipients, ","), dsn))
		out(route.dsnHost)
		out(" failed on DATA command")
		out("\n")
		zerodie()
	}

	err = w.Close()
	msg := err.Error()
	if msg[0] == 49 { // 1Ò
		smtpR := newSmtpResponse(msg[1:])
		if smtpR.code >= 500 {
			out("D")
		} else { // code >=400
			out("Z")
		}
		//out(fmt.Sprintf("%s:%s:%s:", sender, strings.Join(recipients, ","), dsn))
		out(route.dsnHost)
		out(" failed after I sent the message: ")
		out(smtpR.msg)
		out("\n")
		zerodie()
	} else {
		out("K")
		//out(fmt.Sprintf("%s:%s:%s:", sender, strings.Join(recipients, ","), dsn))
		out(route.dsnHost)
		out(" accepted message: ")
		out(msg[1:])
		out("\n")
		zerodie()
	}
	c.Quit()
}

func main() {
	// Parse command-line
	// qmail-remote host sender recip [ recip ... ]
	flag.Parse()
	args := flag.Args()
	if len(args) < 3 {
		dieUsage()
	}
	host := strings.ToLower(args[0])
	sender = strings.ToLower(args[1])
	recipients = args[2:]

	// Read mail from stdin
	data, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		dieRead()
	}
	mailData := string(data)

	// get route
	route := getRoute(sender, host)

	// Send mail in the same order that in recipients list VERY IMPORTANT !!
	sendmail(host, sender, recipients, &mailData, route)

}
