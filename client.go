package xmlrpc

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
	"net/rpc"
	"net/url"
)

// clientCodec is rpc.ClientCodec interface implementation.
type clientCodec struct {
	// url presents url of xmlrpc service
	url *url.URL

	// httpClient works with HTTP protocol
	httpClient *http.Client

	// cookies stores cookies received on last request
	cookies http.CookieJar

	// responses presents map of active requests. It is required to return request id, that
	// rpc.Client can mark them as done.
	responses map[uint64]*http.Response

	response *Response

	// ready presents channel, that is used to link request and it`s response.
	ready chan uint64
}

type Client struct {
	*rpc.Client

	codec *clientCodec
}

func (c *Client) CookieHost() string {
	return c.codec.url.Host
}

func (c *Client) Cookies() []*http.Cookie {
	return c.codec.cookies.Cookies(c.codec.url)
}

func (c *Client) SetCookies(cookies []*http.Cookie) {
	c.codec.cookies.SetCookies(c.codec.url, cookies)
}

func (codec *clientCodec) WriteRequest(request *rpc.Request, args interface{}) (err error) {
	httpRequest, err := NewRequest(codec.url.String(), request.ServiceMethod, args)

	if codec.cookies != nil {
		for _, cookie := range codec.cookies.Cookies(codec.url) {
			httpRequest.AddCookie(cookie)
		}
	}

	if err != nil {
		return err
	}

	var httpResponse *http.Response
	httpResponse, err = codec.httpClient.Do(httpRequest)

	if err != nil {
		return err
	}

	if codec.cookies != nil {
		codec.cookies.SetCookies(codec.url, httpResponse.Cookies())
	}

	codec.responses[request.Seq] = httpResponse
	codec.ready <- request.Seq

	return nil
}

func (codec *clientCodec) ReadResponseHeader(response *rpc.Response) (err error) {
	seq := <-codec.ready
	httpResponse := codec.responses[seq]

	if httpResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("request error: bad status code - %d", httpResponse.StatusCode)
	}

	respData, err := ioutil.ReadAll(httpResponse.Body)

	if err != nil {
		return err
	}

	httpResponse.Body.Close()

	resp := NewResponse(respData)

	if resp.Failed() {
		response.Error = fmt.Sprintf("%v", resp.Err())
	}

	codec.response = resp

	response.Seq = seq
	delete(codec.responses, seq)

	return nil
}

func (codec *clientCodec) ReadResponseBody(v interface{}) (err error) {
	if (v == nil) {
		return nil
	}

	if err = codec.response.Unmarshal(v); err != nil {
		return err
	}

	return nil
}

func (codec *clientCodec) Close() error {
	transport := codec.httpClient.Transport.(*http.Transport)
	transport.CloseIdleConnections()
	return nil
}

// NewClient returns instance of rpc.Client object, that is used to send request to xmlrpc service.
func NewClient(requrl string, transport http.RoundTripper) (*Client, error) {
	if transport == nil {
		transport = http.DefaultTransport
	}

	httpClient := &http.Client{Transport: transport}

	jar, err := cookiejar.New(nil)

	if err != nil {
		return nil, err
	}

	u, err := url.Parse(requrl)

	if err != nil {
		return nil, err
	}

	codec := clientCodec{
		url:        u,
		httpClient: httpClient,
		ready:      make(chan uint64),
		responses:  make(map[uint64]*http.Response),
		cookies:    jar,
	}

	return &Client{rpc.NewClientWithCodec(&codec), &codec}, nil
}
