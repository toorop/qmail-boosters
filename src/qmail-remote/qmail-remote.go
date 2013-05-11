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


	qmail-remote replacement for using mailjet as outgoing SMTP relay 

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
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/Toorop/qmail-boosters/src/smtp"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	ZEROBYTE byte = 0
)

type Route struct {
	name     string
	host     string
	port     string
	username string
	passwd   string
}

type SmtpResponse struct {
	code int
	msg  string
}

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

func dieControl() {
	fmt.Print("ZUnable to read control files. (#4.3.0)\n")
	zerodie()
}

func dieControlRoutes() {
	fmt.Print("ZUnable to read control file 'routes'. Bad format or file not found (#4.3.0)\n")
	zerodie()
}

func dieRead() {
	fmt.Print("ZUnable to read message. (#4.3.0)\n")
	zerodie()
}

func dieBadRcptTo() {
	fmt.Print("ZUnable to parse recipient. (#4.3.0)\n")
	zerodie()
}

func dieBadMailFrom() {
	fmt.Print("ZUnable to parse sender. (#4.3.0)\n")
	zerodie()
}

func dieControlRoutesDefaultAsNameIsForbidden() {
	fmt.Print("ZName 'default' for a route is forbidden (#4.3.0)\n")
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

func tempNoCon() {
	fmt.Print("ZSorry, I wasn't able to establish an SMTP connection. (#4.4.1)\n")
	zerodie()
}

func tempTlsFailed() {
	fmt.Print("ZRemote accept STARTTLS but init TLS failed (#4.4.1)\n")
	zerodie()
}

func permNoMx(host string) {
	fmt.Printf("DSorry, I couldn't find a mail exchanger or IP address for host %s. (#5.4.4)\n", host)
	zerodie()
}

func tempAuthFailure(host, msg string) {
	fmt.Printf("ZAuth failure (perhaps temp) dialing to host %s. %s (#5.4.4)\n", host, msg)
	zerodie()
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

func readControl(ctrlFile string) (lines []string) {
	//file := fmt.Sprintf("../testutils/%s", ctrlFile) // debugging purpose
	file := fmt.Sprintf("/var/qmail/%s", ctrlFile)
	f, err := os.Open(file)
	if err != nil {
		dieControl()
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
	routesC := readControl("control/routes")

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
	return routes
}

func getHeloHost() (heloHost string) {
	heloHost = strings.Trim(readControl("control/me")[0], " ")
	return
}

func getRoute(sender string, remoteHost string) (route Route) {

	// if remote hots is an IP skip test
	if net.ParseIP(remoteHost) != nil {
		route.name = remoteHost
		route.host = remoteHost
		route.port = "25"
		return route
	}

	t := strings.Split(sender, "@")
	if len(t) == 1 { // bounce
		route.name = "default"
		return
	}
	senderHost := t[1]
	routes := getRoutes()
	routesMap := readControl("control/routemap")
	for _, r1 := range routesMap {
		parsed := strings.Split(r1, ":")
		if (parsed[0] == "*" || parsed[0] == senderHost) && (parsed[1] == "*" || parsed[1] == remoteHost) {
			route = routes[parsed[2]]
			break
		}
		route.name = "default"
	}
	return
}

func sendmail(remoteHost string, sender string, recipients []string, data *string, route Route) {
	// use tls ?
	//flagTls := false
	// helloHost
	helloHost := getHeloHost()

	if route.name == "default" {
		// get MX
		mxs, err := net.LookupMX(remoteHost)
		if err != nil {
			permNoMx(remoteHost)
		}
		route.host = mxs[0].Host[0 : len(mxs[0].Host)-1]
		route.port = "25"
	}

	if route.port == "" {
		route.port = "25"
	}

	dsn := fmt.Sprintf("%s:%s", route.host, route.port)

	// Connect
	c, err := smtp.Dial(dsn, helloHost)
	if err != nil {
		tempNoCon()
	}
	defer c.Quit()

	// STARTTLS ?
	if ok, _ := c.Extension("STARTTLS"); ok {
		var config tls.Config
		config.InsecureSkipVerify = true
		if err = c.StartTLS(&config); err != nil {
			tempTlsFailed()
		}
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
				tempAuthFailure(route.host, msg)
			}
		}
	}

	if err := c.Mail(sender); err != nil {
		c.Quit()
		smtpR := newSmtpResponse(err.Error())
		if smtpR.code >= 500 {
			out("D")
		} else {
			out("Z")
		}
		out(fmt.Sprintf("Connected to %s but sender was rejected. %s.\n", route.host, smtpR.msg))
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
			out(route.host)
			out(" does not like recipient.\n")
			out(smtpR.msg)
		} else {
			out("r")
			flagAtLeastOneRecipitentSuccess = true
		}
		zero()
	}

	if !flagAtLeastOneRecipitentSuccess {
		out("DGiving up on ")
		out(route.host)
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
		out(route.host)
		out(" failed on DATA command : ")
		out(smtpR.msg)
		out("\n")
		zerodie()
	}

	buf := bytes.NewBufferString(*data)
	if _, err := buf.WriteTo(w); err != nil {
		out("Z")
		out(route.host)
		out(" failed on DATA command")
		out("\n")
		zerodie()
	}

	err = w.Close()
	if err != nil {
		smtpR := newSmtpResponse(err.Error())
		if smtpR.code >= 500 {
			out("D")
		} else { // code >=400
			out("Z")
		}
		out(route.host)
		out(" failed after I sent the message : ")
		out(smtpR.msg)
		out("\n")
		zerodie()
	} else {
		out("K")
		out(route.host)
		out(" accepted message.")
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
	sender := strings.ToLower(args[1])
	recipients := args[2:]

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
