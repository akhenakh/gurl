package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// usage: http [--json | --form] [--pretty {all,colors,format,none}]
//             [--style STYLE] [--print WHAT | --verbose | --headers | --body]
//             [--stream]
//             [--session SESSION_NAME | --session-read-only SESSION_NAME]
//             [--auth USER[:PASS]] [--auth-type {basic,digest}]
//             [--proxy PROTOCOL:HOST] [--follow] [--verify VERIFY]
//             [--timeout SECONDS] [--check-status] [--help] [--version]
//             [--traceback] [--debug]
//             [METHOD] URL [REQUEST ITEM [REQUEST ITEM ...]]

var jsonFlag = flag.Bool("json", false, "Data items from the command line are serialized as a JSON object.\nThe Content-Type and Accept headers are set to application/json (if not specified).")
var formFlag = flag.Bool("form", true, " Data items are serialized as form fields. The Content-Type is set to application/x-www-form-urlencoded (if not specifid).\nThe presence of any file fields results into a multipart/form-data request.")
var verboseFlag = flag.Bool("verbose", false, "Print the whole request as well as the response.")
var indentFlag = flag.Bool("indent", true, "Indent known formats like JSON.")
var versionFlag = flag.Bool("version", false, "Return version and exit")
var authTypeFlag = flag.String("auth-type", "basic", "Set the authentication type, basic|oauth1_2l")
var authFlag = flag.String("auth", "", "Authentication USER:PASS")
var serverFlag = flag.String("server", "", "Hijack the server and port from url and connect to SERVER:PORT then send the real Host, usefull to debug with load balancer. ")

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

func sortHeader(m http.Header) []string {
	// http.Header is a map[string][]string
	// map is already a reference type no need for a pointer
	mk := make([]string, len(m))
	i := 0
	for k, _ := range m {
		mk[i] = k
		i++
	}
	sort.Strings(mk)
	return mk
}

func isJSON(m http.Header) bool {
	if content_type := m["Content-Type"]; content_type != nil {
		for _, v := range content_type {
			// Test for Content-Type: application/json; charset=utf-8 case
			if strings.Contains(v, "application/json") {
				return true
			}
		}
	}
	return false
}

func main() {
	flag.Parse()

	var args []string
	for _, value := range os.Args {
		if value[0] != '-' {
			args = append(args, value)
		}
	}

	if *versionFlag {
		fmt.Println("v0.1a")
		os.Exit(0)
	}
	if len(args) < 3 {
		msg := fmt.Sprintf("Invalid usage for %s\n", filepath.Base(args[0]))
		log.Fatal(msg)
	}

	if (*formFlag && *jsonFlag) || (!*formFlag && !*jsonFlag) {
		log.Fatal("Invalid usage json and form flag are mutually exclusive")
	}

	if *authTypeFlag != "basic" && *authTypeFlag != "digest" {
		log.Fatal("Auth type is invalid")
	}

	url_req, err := url.Parse(args[2])
	if err != nil || url_req.Scheme[0:4] != "http" {
		msg := fmt.Sprintf("Invalid URL %s\n", args[2])
		log.Fatal(msg)
	}

	method := strings.ToUpper(args[1])
	var req_body string

	req, err := http.NewRequest(method, args[2], nil)
	if err != nil {
		log.Fatal(err)
	}

	if *authFlag != "" {
		split_auth := strings.Split(*authFlag, ":")
		if len(split_auth) != 2 {
			log.Fatal("Invalid syntax for auth: ", *authFlag)
		}

		if *authFlag == "basic" {
			req.SetBasicAuth(split_auth[0], split_auth[1])
		} else if *authFlag == "oauth1_2l" {

		}
		// future usage for oauth1 2legged
	}

	// test allowed methods
	switch method {
	case "GET", "DELETE", "HEAD", "OPTIONS":
	case "POST", "PUT":
		// encode all values
		if len(args) < 4 {
			break
		}

		req_body_tab := make([]string, 1)
		for _, param := range args[3:] {
			if !strings.ContainsAny(param, ": @ =") {
				log.Fatal("Invalid parameter ", param)
			}
			// = form case
			split_param := strings.Split(param, "=")
			if len(split_param) > 2 {
				log.Fatal("Invalid parameter ", param)
			} else if len(split_param) == 2 {
				req_body_tab = append(req_body_tab, url.QueryEscape(split_param[0])+"="+url.QueryEscape(split_param[1]))
			}

			// : header case
			split_param = strings.Split(param, ":")
			if len(split_param) > 2 {
				log.Fatal("Invalid parameter ", param)
			} else if len(split_param) == 2 {
				req.Header.Add(split_param[0], split_param[1])
			}

		}
		if len(req_body_tab) > 1 {
			req_body = strings.Join(req_body_tab, "&")
		} else if len(req_body_tab) == 1 {
			req_body = req_body_tab[0]
		}

	default:
		log.Fatal("Invalid method")
	}

	req.Body = nopCloser{bytes.NewBufferString(req_body)}

	if *formFlag {
		req.Header.Add(`Content-Type`, `application/x-www-form-urlencoded; charset=utf-8`)
	}

	req.Header.Add(`User-Agent`, `Gurl`)

	// Hijack the connection if needed
	host := url_req.Host
	if *serverFlag != "" {
		host = *serverFlag
	}
	
	// add the default port if missing
	split_host := strings.Split(host, ":")
	if len(split_host) == 1 {
		host += ":80"
	}

	client_tcp_conn, err := net.Dial("tcp", host)
	if err != nil {
		log.Fatal(err)
	}

	client_http_conn := httputil.NewClientConn(client_tcp_conn, nil)

	resp, err := client_http_conn.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	req_dump, err := httputil.DumpRequest(req, true)
	if err != nil {
		log.Fatal(err)
	}

	if *verboseFlag {
		fmt.Printf("%s", req_dump)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	/*resp_dump, err := httputil.DumpResponse(resp, true)
	if err != nil {
		log.Fatal(err)
	} 
	fmt.Printf("%s\n", resp_dump) */

	for _, k := range sortHeader(resp.Header) {
		fmt.Printf("%s: %s\n", k, strings.Join(resp.Header[k], ", "))
	}

	// Indent JSON code if needed
	if *indentFlag && isJSON(resp.Header) {
		arr := make([]byte, 0, 1024*1024)
		buf := bytes.NewBuffer(arr)
		err := json.Indent(buf, body, "", "    ")
		if err != nil {
			fmt.Printf("\n%s\n\n", body)
			log.Fatal(err)
		}
		fmt.Printf("\n%s\n", buf)
	} else {
		fmt.Printf("\n%s\n", body)
	}

}
