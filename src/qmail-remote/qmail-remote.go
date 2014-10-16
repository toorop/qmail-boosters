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
		# Name;LocalAddresses;RemotesAddresses;username;passwd
		mailjet:in.mailjet.com:25:username:password
		pm:mx5.protecmail.com:25::

    	routemap
    	# comment
		#senderHost;recipientHost;routeName
		*:protecmail.com:pm
		*:toorop.fr:pm
		*:ovh.com:mx1.ovh.net
		*:*:mailjet

		defaultoutgoingip
		111.111.111.111
*/

/*
TODO
- 2014-09-08 04:03:04.431716500 delivery 1503: deferral: Sorry_but_i_don't_understand_SMTP_response_:_EOF_/


*/
package main

import (
	"bufio"
	"bytes"
	"container/list"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"github.com/Toorop/qmail-boosters/src/smtp"
	"io/ioutil"
	"math/rand"
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
	name  string
	rAddr string // remote IPs or Hostnames
	//rPort    string
	lAddr    string // local ourtgoing IPs
	username string
	passwd   string
	qrHost   string // host in qmail-remote cmd
}

type SmtpResponse struct {
	code int
	msg  string
}

type smtpDialresult struct {
	client *smtp.Client
	err    error
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

func dieControl(msg string) {
	fmt.Printf("Z%s:%s:%s:Unable to read control files: %s (#4.3.0)\n", qbUuid, sender, strings.Join(recipients, ","), msg)
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

func tempNoCon(dsn string, err error) {
	fmt.Printf("Z%s:%s:%s:Sorry, I wasn't able to establish an SMTP connection to remote host(s) %s. %s (#4.4.1)\n", qbUuid, sender, strings.Join(recipients, ","), dsn, err)
	zerodie()
}

func tempTimeout(dsn string) {
	fmt.Printf("Z%s:%s:%s:Sorry, timeout occured while speaking to %s. (#4.4.1)\n", qbUuid, sender, strings.Join(recipients, ","), dsn)
	zerodie()
}

func tempTlsFailed(lAddr, dsn, msg string) {
	fmt.Printf("Z%s:%s->%s:%s:%s:Remote host accept STARTTLS but init TLS failed - %s(#4.4.1)\n", qbUuid, lAddr, dsn, sender, strings.Join(recipients, ","), msg)
	zerodie()
}

func permNoMx(host string) {
	fmt.Printf("D%s:%s:%s:Sorry, I couldn't find a mail exchanger or IP address for host %s. (#5.4.4)\n", qbUuid, sender, strings.Join(recipients, ","), host)
	zerodie()
}

func permResolveHostFailed(host string) {
	fmt.Printf("D%s:%s:%s:Sorry, I couldn't resolve this hostname %s. (#5.4.4)\n", qbUuid, sender, strings.Join(recipients, ","), host)
	zerodie()
}

func permNoInterface(iface string) {
	fmt.Printf("D%s:%s:%s:Sorry, I couldn't find local interface %s. (#5.4.4)\n", qbUuid, sender, strings.Join(recipients, ","), iface)
	zerodie()
}

func permDebug(msg string) {
	fmt.Printf("D%s\n", msg)
	zerodie()
}

func tempAuthFailure(lAddr, dsn, msg string) {
	fmt.Printf("Z%s:%s->%s:%s:%s:Auth failure (perhaps temp) dialing to host : %s (#5.4.4)\n", qbUuid, lAddr, dsn, sender, strings.Join(recipients, ","), msg)
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

func readControl(ctrlFile string) (lines []string) {
	//file := fmt.Sprintf("../testutils/%s", ctrlFile) // debugging purpose
	file := fmt.Sprintf("/var/qmail/%s", ctrlFile)
	f, err := os.Open(file)
	if err != nil {
		dieControl(file)
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

func getHeloHost() (heloHost string) {
	t := readControl("control/me")
	heloHost = strings.Trim(t[0], " ")
	return
}

func getRoute(sender string, remoteHost string) (route Route) {
	var senderHost string

	route.name = "default"
	route.qrHost = remoteHost

	// if remotehost is an IP skip test
	if net.ParseIP(remoteHost) != nil {
		route.name = remoteHost
		route.rAddr = fmt.Sprintf("%s:25", remoteHost)
		return
	}

	t := strings.Split(sender, "@")
	if len(t) == 1 { // bounce
		senderHost = "bounce"
	} else {
		senderHost = t[1]
	}

	//routes := getRoutes()
	// Find route name in route map
	routesMap := readControl("control/routemap")
	for _, r1 := range routesMap {
		parsed := strings.Split(r1, ";")
		if (parsed[0] == "*" || parsed[0] == senderHost) && (parsed[1] == "*" || parsed[1] == remoteHost) {
			route.name = parsed[2]
			break
		}
	}

	// Find route from control/routes
	routesC := readControl("control/routes")
	for _, strRoute := range routesC {
		parsedRoute := strings.Split(strRoute, ";")
		if parsedRoute[0] == route.name {
			route.lAddr = parsedRoute[1]
			route.rAddr = parsedRoute[2]
			route.username = parsedRoute[3]
			route.passwd = parsedRoute[4]
			break
		}
	}

	// Route found
	if route.name != "default" && route.rAddr != "" {
		return
	}

	// try to find route in smtproutes
	smtproutes := readControl("control/smtproutes")
	for _, l := range smtproutes {
		s := strings.Split(l, ":")
		if s[0] == remoteHost {
			route.name = "smtproutes"
			route.rAddr = fmt.Sprintf("%s:25", s[1])
			break
		}
	}
	return
}

// Return route form MX records
// cat = failover | roundrobin
func getMxRoute(host, sep string) (route string) {
	mxs, err := net.LookupMX(host)
	if err != nil {
		route = host
	} else {
		for i, mx := range mxs {
			if i == 0 {
				route = fmt.Sprintf("%s:25", mx.Host[0:len(mx.Host)-1])
			} else {
				route = fmt.Sprintf("%s%s%s:25", route, sep, mx.Host[0:len(mx.Host)-1])
			}
		}
	}
	return
}

// toto.com:25 -> 111.111.111.111:25
func hostPortToIpPort(dsnHost string) (dnsIp string) {
	host, port, _ := net.SplitHostPort(dsnHost)
	// If hostname get IP
	if net.ParseIP(host) == nil {
		t, err := net.LookupHost(host)
		if err != nil {
			permResolveHostFailed(host)
		} else {
			return net.JoinHostPort(t[0], port)
		}
	}
	return dsnHost
}

func getDefaultLocalAddr() (lAddr string) {
	ip := readControl("control/defaultoutgoingip")
	if len(ip) < 1 {
		dieControl("Bad format for defaultOutgoingIp file")
	}
	return ip[0]
}

func newSmtpClient(route Route) (client *smtp.Client, err error) {
	var tAddrs []string // temp address slices
	var heloHost string
	lAddrs := list.New() // Local addresses
	rAddrs := list.New() // Remote addresses

	// remote dsn (ip:port)
	//rdsn := fmt.Sprintf("%s:%s", route.rAddr, route.rPort)

	///////////////////////////////
	// Locals address

	// Si il n'y a pas d'adresse locale il faut prendre celle de la eth0
	if route.lAddr == "" {
		route.lAddr = getDefaultLocalAddr()
	}

	// failover ?
	tAddrs = strings.Split(route.lAddr, "&")
	if len(tAddrs) > 1 {
		for _, a := range tAddrs {
			// If hostname, get IP
			lAddrs.PushBack(a)
		}
	}
	// round robin
	if lAddrs.Len() == 0 { // no failover
		tAddrs = strings.Split(route.lAddr, "|")
		if len(tAddrs) > 1 {
			var i int
			rand.Seed(time.Now().UTC().UnixNano())
			for len(tAddrs) > 0 {
				i = rand.Intn(len(tAddrs))
				lAddrs.PushBack(tAddrs[i])
				tAddrs = append(tAddrs[:i], tAddrs[i+1:]...)
			}
		}
	}
	// Unique IP
	if lAddrs.Len() == 0 {
		lAddrs.PushBack(route.lAddr)
	}

	///////////////////////////////
	// Remote address
	// If no route specified use MX
	if route.rAddr == "" || route.rAddr == "mx" {
		route.rAddr = getMxRoute(route.qrHost, "&")
	}

	// failover ?
	tAddrs = strings.Split(route.rAddr, "&")
	if len(tAddrs) > 1 {
		for _, tAddr := range tAddrs {
			if tAddr == "mx" {
				mxs, err := net.LookupMX(route.qrHost)
				if err != nil {
					rAddrs.PushBack(net.JoinHostPort(route.qrHost, "25"))
				} else {
					for _, mx := range mxs {
						rAddrs.PushBack(net.JoinHostPort(mx.Host[0:len(mx.Host)-1], "25"))
					}
				}
			} else {
				rAddrs.PushBack(hostPortToIpPort(tAddr))
			}

		}
	}

	// round robin
	if rAddrs.Len() == 0 { // -> no failover
		tAddrs = strings.Split(route.rAddr, "|")
		if len(tAddrs) > 1 {
			var i int
			rand.Seed(time.Now().UTC().UnixNano())
			for len(tAddrs) > 0 {
				i = rand.Intn(len(tAddrs))
				rAddrs.PushBack(hostPortToIpPort(tAddrs[i]))
				tAddrs = append(tAddrs[:i], tAddrs[i+1:]...)
			}
		}
	}
	// Unique IP/hostname
	if rAddrs.Len() == 0 { // -> no failover & no roud robin
		rAddrs.PushBack(hostPortToIpPort(route.rAddr))
	}

	// Test all remote Host
	for rAddr := rAddrs.Front(); rAddr != nil; rAddr = rAddr.Next() {
		rHost, rPort, _ := net.SplitHostPort(rAddr.Value.(string))
		//  Try all r address
		for lAddr := lAddrs.Front(); lAddr != nil; lAddr = lAddr.Next() {
			// Get HELO host
			heloHosts, err := net.LookupAddr(lAddr.Value.(string))
			if err != nil {
				heloHost = getHeloHost()
			} else {
				heloHost = heloHosts[0]
				// Remove trailing dot
				// Exchange doesn't like absolute FDQN
				if heloHost[len(heloHost)-1] == 46 {
					heloHost = heloHost[0 : len(heloHost)-2]
				}
			}
			// Dial timeout
			/*connectTimer := time.NewTimer(10 * time.Second)
			done := make(chan smtpDialresult, 1)
			go func() {
				client, err = smtp.Dial(net.JoinHostPort(rHost, rPort), lAddr.Value.(string), heloHost)
				done <- smtpDialresult{client, err}
			}()

			// Wait for the read or the timeout
			select {
			case r := <-done:
				if r.err == nil {
					return r.client, r.err
				}
			// Timeout
			case <-connectTimer.C:
				continue
			}*/

			client, err = smtp.Dial(net.JoinHostPort(rHost, rPort), lAddr.Value.(string), heloHost, 10)
			if err == nil {
				return client, err
			}
		}
	}

	// Teste toutes les adresses locales sir tous les remotes
	for lAddr := lAddrs.Front(); lAddr != nil; lAddr = lAddr.Next() {
		// Get HELO host
		heloHosts, err := net.LookupAddr(lAddr.Value.(string))
		if err != nil {
			heloHost = getHeloHost()
		} else {
			heloHost = heloHosts[0]
		}
		//  Try all r address
		//  //rdsn := fmt.Sprintf("%s:%s", route.rAddr, route.rPort)
		for rAddr := rAddrs.Front(); rAddr != nil; rAddr = rAddr.Next() {
			rHost, rPort, _ := net.SplitHostPort(rAddr.Value.(string))
			// TODO Dial timeout
			client, err = smtp.Dial(fmt.Sprintf("%s:%s", rHost, rPort), lAddr.Value.(string), heloHost, 10)
			if err == nil {
				return client, err
			}
		}
	}
	return client, errors.New("All local address have been tested")
}

func sendmail(sender string, recipients []string, data *string, route Route) {
	// Extract qmail-booster UUID from header (need qmail-booster version of qmail-smtpd (coming soon))
	bufh := bytes.NewBufferString(*data)
	mailmsg, e := mail.ReadMessage(bufh)
	if e == nil {
		qbUuid = mailmsg.Header.Get("X-QB-UUID")
	}
	if qbUuid == "" {
		qbUuid = "nouuid" // default
	}

	// Timeout connect 240 seconds
	// TODO c'est trop court car si on a une sortie bloquée le dial va lui même se mettre
	// en timoute au bout de X secondes
	timeoutCon := make(chan bool, 1)
	go timeout(timeoutCon, 240)
	go doTimeout(timeoutCon, fmt.Sprintf("%s", route.rAddr))

	// Connect
	c, err := newSmtpClient(route)

	if err != nil {
		tempNoCon(fmt.Sprintf("%s", route.rAddr), err)
	}
	dsn := fmt.Sprintf("%s:%s", c.Raddr, c.Rport)
	defer c.Quit()
	// Timeout sessions TODO

	// STARTTLS ?
	// 2013-06-22 14:19:30.670252500 delivery 196893: deferral: Sorry_but_i_don't_understand_SMTP_response_:_local_error:_unexpected_message_/
	// 2013-06-18 10:08:29.273083500 delivery 856840: deferral: Sorry_but_i_don't_understand_SMTP_response_:_failed_to_parse_certificate_from_server:_negative_serial_number_/
	// https://code.google.com/p/go/issues/detail?id=3930
	if ok, _ := c.Extension("STARTTLS"); ok {
		var config tls.Config
		config.InsecureSkipVerify = true
		// If TLS nego failed bypass secure transmission
		err = c.StartTLS(&config)
		if err != nil { // fallback to no TLS
			c.Quit()
			c, err = newSmtpClient(route)
			if err != nil {
				tempNoCon(dsn, err)
			}
			defer c.Quit()
			//tempTlsFailed(c.Laddr, dsn, err.Error())
		}
	}

	// Auth
	var auth smtp.Auth
	if route.username != "" && route.passwd != "" {
		_, auths := c.Extension("AUTH")

		if strings.Contains(auths, "CRAM-MD5") {
			auth = smtp.CRAMMD5Auth(route.username, route.passwd)
		} else { // PLAIN
			auth = smtp.PlainAuth("", route.username, route.passwd, route.rAddr)
		}
	}

	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			err := c.Auth(auth)
			if err != nil {
				msg := fmt.Sprintf("%s", err)
				tempAuthFailure(c.Laddr, dsn, msg)
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
		out(fmt.Sprintf("%s:%s->%s:%s:%s:Connected to remote host but sender was rejected. %s.\n", qbUuid, c.Laddr, dsn, sender, strings.Join(recipients, ","), smtpR.msg))
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
			out(fmt.Sprintf("%s:%s->%s:%s:%s:", qbUuid, c.Laddr, dsn, sender, rcptto))
			out(" does not like recipient.")
			out(smtpR.msg)
		} else {
			out("r")
			out(fmt.Sprintf("%s:%s->%s:%s:%s:recipient accepted.", qbUuid, c.Laddr, dsn, sender, rcptto))
			flagAtLeastOneRecipitentSuccess = true
		}
		zero()
	}

	if !flagAtLeastOneRecipitentSuccess {
		out("D")
		//out(fmt.Sprintf("%s:%s:%s:", sender, strings.Join(recipients, ","), dsn))
		out("Giving up on ")
		out(route.rAddr)
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
		//out(fmt.Sprintf("%s:%s->%s:%s:%s:", qbUuid, c.Laddr, dsn, sender, strings.Join(recipients, ",")))
		out(" failed on DATA command : ")
		out(smtpR.msg)
		out("\n")
		zerodie()
	}

	buf := bytes.NewBufferString(*data)
	if _, err := buf.WriteTo(w); err != nil {
		out("Z")
		//out(fmt.Sprintf("%s:%s->%s:%s:%s:", qbUuid, c.Laddr, dsn, sender, strings.Join(recipients, ",")))
		//out(route.rAddr)
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
		//out(fmt.Sprintf("%s:%s->%s:%s:%s:", qbUuid, c.Laddr, dsn, sender, strings.Join(recipients, ",")))
		//out(route.rAddr)
		out(" failed after I sent the message: ")
		out(smtpR.msg)
		out("\n")
		zerodie()
	} else {
		out("K")
		//out(fmt.Sprintf("%s:%s->%s:%s:%s:", qbUuid, c.Laddr, dsn, sender, strings.Join(recipients, ",")))
		//out(route.rAddr)
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
	sendmail(sender, recipients, &mailData, route)

}
