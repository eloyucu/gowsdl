package soap

import (
	"bytes"
	"encoding/xml"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/andreyvit/diff"
	"github.com/clbanning/mxj"
	"github.com/tdewolff/minify"
	xmlminify "github.com/tdewolff/minify/xml"
)

type Ping struct {
	XMLName xml.Name `xml:"http://example.com/service.xsd Ping"`

	Request *PingRequest `xml:"request,omitempty"`
}

type PingRequest struct {
	// XMLName xml.Name `xml:"http://example.com/service.xsd PingRequest"`

	Message    string  `xml:"Message,omitempty"`
	Attachment *Binary `xml:"Attachment,omitempty"`
}

type PingResponse struct {
	XMLName xml.Name `xml:"http://example.com/service.xsd PingResponse"`

	PingResult *PingReply `xml:"PingResult,omitempty"`
}

type PingReply struct {
	// XMLName xml.Name `xml:"http://example.com/service.xsd PingReply"`

	Message    string `xml:"Message,omitempty"`
	Attachment []byte `xml:"Attachment,omitempty"`
}

func TestClient_Call(t *testing.T) {
	var pingRequest = new(Ping)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		xml.NewDecoder(r.Body).Decode(pingRequest)
		rsp := `<?xml version="1.0" encoding="utf-8"?>
		<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema">
			<soap:Body>
				<PingResponse xmlns="http://example.com/service.xsd">
					<PingResult>
						<Message>Pong hi</Message>
					</PingResult>
				</PingResponse>
			</soap:Body>
		</soap:Envelope>`
		w.Write([]byte(rsp))
	}))
	defer ts.Close()

	client := NewClient(ts.URL)
	req := &Ping{Request: &PingRequest{Message: "Hi"}}
	reply := &PingResponse{}
	if err := client.Call("GetData", req, reply); err != nil {
		t.Fatalf("couln't call service: %v", err)
	}

	wantedMsg := "Pong hi"
	if reply.PingResult.Message != wantedMsg {
		t.Errorf("got msg %s wanted %s", reply.PingResult.Message, wantedMsg)
	}
}

func TestClient_Send_Correct_Headers(t *testing.T) {
	tests := []struct {
		action          string
		reqHeaders      map[string]string
		expectedHeaders map[string]string
	}{
		// default case when no custom headers are set
		{
			"GetTrade",
			map[string]string{},
			map[string]string{
				"User-Agent":   "gowsdl/0.1",
				"SOAPAction":   "GetTrade",
				"Content-Type": "text/xml; charset=\"utf-8\"",
			},
		},
		// override default User-Agent
		{
			"SaveTrade",
			map[string]string{"User-Agent": "soap/0.1"},
			map[string]string{
				"User-Agent": "soap/0.1",
				"SOAPAction": "SaveTrade",
			},
		},
		// override default Content-Type
		{
			"SaveTrade",
			map[string]string{"Content-Type": "text/xml; charset=\"utf-16\""},
			map[string]string{"Content-Type": "text/xml; charset=\"utf-16\""},
		},
	}

	var gotHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header
	}))
	defer ts.Close()

	for _, test := range tests {
		client := NewClient(ts.URL, WithHTTPHeaders(test.reqHeaders))
		req := struct{}{}
		reply := struct{}{}
		client.Call(test.action, req, reply)

		for k, v := range test.expectedHeaders {
			h := gotHeaders.Get(k)
			if h != v {
				t.Errorf("got %s wanted %s", h, v)
			}
		}
	}
}

func TestClient_MTOM(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			w.Header().Set(k, v[0])
		}
		bodyBuf, _ := ioutil.ReadAll(r.Body)
		w.Write(bodyBuf)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, WithMTOM())
	req := &PingRequest{Attachment: NewBinary([]byte("Attached data")).SetContentType("text/plain")}
	reply := &PingRequest{}
	if err := client.Call("GetData", req, reply); err != nil {
		t.Fatalf("couln't call service: %v", err)
	}

	if !bytes.Equal(reply.Attachment.Bytes(), req.Attachment.Bytes()) {
		t.Errorf("got %s wanted %s", reply.Attachment.Bytes(), req.Attachment.Bytes())
	}

	if reply.Attachment.ContentType() != req.Attachment.ContentType() {
		t.Errorf("got %s wanted %s", reply.Attachment.Bytes(), req.Attachment.ContentType())
	}
}

func TestGetEnvelope(t *testing.T) {
	// Credentials is Credentials
	type Credentials struct {
		XMLName  xml.Name `xml:"http://www.namespace.ninja Credentials,omitempty" json:"-"`
		Login    string   `xml:"Login" json:"login" jsonschema:"required, title: Login"`
		Password string   `xml:"Password" json:"password" jsonschema:"required, title: Password"`
	}
	type Item struct {
		Type  string `xml:"type,attr,omitempty"`
		Value string `xml:",omitempty"`
	}
	type MessageRequest struct {
		XMLName    xml.Name `xml:"http://www.midoco.de/order MessageRequest"`
		FirstItem  Item
		SecondItem []Item
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	SOAPClient := NewClient(ts.URL)
	SOAPClient.AddHeader(Credentials{
		Login:    "login_value",
		Password: "password_value",
	})
	body := MessageRequest{
		FirstItem: Item{
			Type:  "item_1",
			Value: "value_1",
		},
		SecondItem: []Item{
			{
				Type:  "item_2_1",
				Value: "value_2_1",
			},
			{
				Type:  "item_2_2",
				Value: "value_2_2",
			},
		},
	}
	envelope, err := SOAPClient.GetRequest(body)
	if err != nil {
		t.Errorf("Something went wrong trying to get the request (GetRequest): %v", err)
	} else {
		output, err := xml.MarshalIndent(envelope, "  ", "    ")
		if err != nil {
			t.Errorf("Something went wrong trying to marshal request: %v", err)
		}

		expected := `<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/">
			<Header xmlns="http://schemas.xmlsoap.org/soap/envelope/">
				<Credentials xmlns="http://www.namespace.ninja">
					<Login>login_value</Login>
					<Password>password_value</Password>
				</Credentials>
			</Header>
			<Body xmlns="http://schemas.xmlsoap.org/soap/envelope/">
				<MessageRequest xmlns="http://www.midoco.de/order">
					<FirstItem type="item_1">
						<Value>value_1</Value>
					</FirstItem>
					<SecondItem type="item_2_1">
						<Value>value_2_1</Value>
					</SecondItem>
					<SecondItem type="item_2_2">
						<Value>value_2_2</Value>
					</SecondItem>
				</MessageRequest>
			</Body>
		</Envelope>`
		if !compareXMLs(expected, string(output)) {
			a, _ := mxj.BeautifyXml([]byte(expected), "", "  ")
			b, _ := mxj.BeautifyXml(output, "", "  ")
			aStr := string(a)
			bStr := string(b)
			// ioutil.WriteFile(tt.outputXMLFile, []byte(prettifyXML(midocoReqBody)), os.ModePerm)
			t.Errorf("Output differ from a golden file: \n%v", diff.LineDiff(aStr, bStr))
		}
	}

}

func compareXMLs(output, expected string) bool {
	m := minify.New()

	r := bytes.NewBufferString(output)
	w := &bytes.Buffer{}
	xmlminify.Minify(m, w, r, nil)
	output = w.String()

	w = &bytes.Buffer{}
	r = bytes.NewBufferString(expected)
	xmlminify.Minify(m, w, r, nil)
	expected = w.String()

	if output == expected {
		return true
	}
	return false
}
