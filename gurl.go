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
	"strings"
)

//TODO 
// proxy
// oauthv1 2legged
// multipart/form-data
// file support
// timeout

var jsonFlag = flag.Bool("json", false, "Data items from the command line are serialized as a JSON object.\nThe Content-Type and Accept headers are set to application/json (if not specified).")
var formFlag = flag.Bool("form", true, " Data items are serialized as form fields. The Content-Type is set to application/x-www-form-urlencoded (if not specifid).\nThe presence of any file fields results into a multipart/form-data request.")
var verboseFlag = flag.Bool("verbose", false, "Print the whole request as well as the response.")
var noindentFlag = flag.Bool("noindent", false, "Do not indent known formats like JSON.")
var versionFlag = flag.Bool("version", false, "Return version and exit")
var authTypeFlag = flag.String("auth-type", "basic", "Set the authentication type, basic|oauth1_2l")
var authFlag = flag.String("auth", "", "Authentication USER:PASS")
var serverFlag = flag.String("server", "", "Connect to SERVER:PORT instead of the ones in the url then send the real Host, usefull to debug with load balancer. ")

type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

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

	if *jsonFlag {
		*formFlag = false
	}

	if *authTypeFlag != "basic" && *authTypeFlag != "digest" {
		log.Fatal("Auth type is invalid")
	}

	url_req, err := url.Parse(args[2])
	if url_req.Scheme == "" {
		new_url := "http://" + args[2]
		url_req, err = url.Parse(new_url)
	}
	if err != nil {
		msg := fmt.Sprintf("Invalid URL %s\n", args[2])
		log.Fatal(msg)
	}

	method := strings.ToUpper(args[1])
	var req_body string

	req, err := http.NewRequest(method, url_req.String(), nil)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add(`User-Agent`, `Gurl`)
	req.Header.Add(`Accept`, `*/*`)

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
	// usefull hack to make this validated like a if x in [a, b]
	case "GET", "DELETE", "HEAD", "OPTIONS":
	case "POST", "PUT":
		if *formFlag == true {
			req.Header.Add(`Content-Type`, `application/x-www-form-urlencoded; charset=utf-8`)
		} else {
			req.Header.Add(`Content-Type`, `application/json`)
		}

		// encode all values
		if len(args) < 4 {
			break
		}

		req_body_map := map[string]string{}

		for _, param := range args[3:] {
			if !strings.ContainsAny(param, ": @ =") {
				log.Fatal("Invalid parameter ", param)
			}
			// = params case
			split_param := strings.Split(param, "=")
			if len(split_param) > 2 {
				log.Fatal("Invalid parameter ", param)
			} else if len(split_param) == 2 {
				req_body_map[split_param[0]] = split_param[1]
			}
		}
		if *formFlag {
			for k, v := range req_body_map {
				req_body += url.QueryEscape(k) + "=" + url.QueryEscape(v) + "&"
			}
		} else {
			// json encode
			b, err := json.Marshal(req_body_map)
			if err != nil {
				log.Fatal(err)
			}
			req_body = string(b)
		}

	default:
		log.Fatal("Invalid method")
	}

	for _, param := range args[3:] {
		// : header case
		split_param := strings.Split(param, ":")
		if len(split_param) > 2 {
			log.Fatal("Invalid parameter ", param)
		} else if len(split_param) == 2 {
			// case Accept: to remove the default accept
			if split_param[1] == "" {
				req.Header.Del(split_param[0])
			} else {
				req.Header.Set(split_param[0], split_param[1])
			}
		}
	}

	req.Body = nopCloser{bytes.NewBufferString(req_body)}

	// Hijack the host if needed
	host := url_req.Host
	if *serverFlag != "" {
		host = *serverFlag
	}

	// add the default port if missing
	split_host := strings.Split(host, ":")
	if len(split_host) == 1 {
		host += ":80"
	}

	// create the connection
	client_tcp_conn, err := net.Dial("tcp", host)
	if err != nil {
		log.Fatal(err)
	}

	client_http_conn := httputil.NewClientConn(client_tcp_conn, nil)

	resp, err := client_http_conn.Do(req)
	if err != nil {
		// a Connection: close is not an error
		if err != httputil.ErrPersistEOF {
			log.Fatal(err)
		}
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

	resp_dump, err := httputil.DumpResponse(resp, false)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s", resp_dump)

	// Indent JSON code if needed
	if !*noindentFlag && isJSON(resp.Header) {
		//TODO: allocated twice the size of the body ?
		arr := make([]byte, 0, 1024*1024)
		buf := bytes.NewBuffer(arr)
		err := json.Indent(buf, body, "", "    ")
		if err != nil {
			fmt.Printf("%s\n\n", body)
			log.Fatal(err)
		}
		fmt.Printf("%s\n", buf)
	} else {
		fmt.Printf("%s\n", body)
	}

}
