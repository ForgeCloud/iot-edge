/*
 * Copyright 2020 ForgeRock AS
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package things

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"github.com/go-ocf/go-coap"
	"github.com/go-ocf/go-coap/codes"
	"github.com/go-ocf/go-coap/net"
	"github.com/pion/dtls/v2"
	"golang.org/x/sync/errgroup"
	"testing"
)

func testGenerateSigner() crypto.Signer {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return key
}

func testAuthCOAPMux(code codes.Code, response []byte) (mux *coap.ServeMux) {
	mux = coap.NewServeMux()
	mux.HandleFunc("/authenticate", func(w coap.ResponseWriter, r *coap.Request) {
		w.SetCode(code)
		w.Write(response)
		return
	})
	return mux
}

func testAMInfoCOAPMux(code codes.Code, response []byte) (mux *coap.ServeMux) {
	mux = coap.NewServeMux()
	mux.HandleFunc("/aminfo", func(w coap.ResponseWriter, r *coap.Request) {
		w.SetCode(code)
		w.Write(response)
		return
	})
	return mux
}

func testAccessTokenCOAPMux(code codes.Code, response []byte) (mux *coap.ServeMux) {
	mux = coap.NewServeMux()
	mux.HandleFunc("/accesstoken", func(w coap.ResponseWriter, r *coap.Request) {
		w.SetCode(code)
		w.Write(response)
		return
	})
	return mux
}

type testCOAPServer struct {
	config *dtls.Config
	mux    *coap.ServeMux
}

func (s testCOAPServer) Start() (address string, cancel func(), err error) {
	l, err := net.NewDTLSListener("udp", ":0", s.config, heartBeat)
	if err != nil {
		return "", func() {}, err
	}
	server := &coap.Server{
		Listener: l,
		Handler:  s.mux,
	}
	c := make(chan error, 1)
	go func() {
		c <- server.ActivateAndServe()
		l.Close()
	}()
	return l.Addr().String(), func() {
		server.Shutdown()
		<-c
	}, nil
}

func testGatewayClientInitialise(client *GatewayClient, server *testCOAPServer) (err error) {
	if server != nil {
		var cancel func()
		client.Address, cancel, err = server.Start()
		if err != nil {
			panic(err)
		}
		defer cancel()
	}

	return client.initialise()
}

func TestGatewayClient_Initialise(t *testing.T) {
	cert, _ := publicKeyCertificate(testGenerateSigner())

	tests := []struct {
		name       string
		successful bool
		client     *GatewayClient
		server     *testCOAPServer
	}{
		{name: "success", successful: true, client: &GatewayClient{Key: testGenerateSigner()}, server: &testCOAPServer{config: dtlsServerConfig(cert), mux: coap.DefaultServeMux}},
		{name: "client-no-signer", client: &GatewayClient{Key: nil}, server: nil},
		// starting a DTLS server without a certificate or PSK is an error.
		{name: "server-wrong-tls-signer", client: &GatewayClient{Key: testGenerateSigner()}, server: &testCOAPServer{config: dtlsServerConfig(testWrongTLSSigner()), mux: coap.DefaultServeMux}},
	}
	for _, subtest := range tests {
		t.Run(subtest.name, func(t *testing.T) {
			err := testGatewayClientInitialise(subtest.client, subtest.server)
			if subtest.successful && err != nil {
				t.Error(err)
			}
			if !subtest.successful && err == nil {
				t.Error("Expected an error")
			}
		})
	}
}

// checks that multiple Thing Gateway Clients can be initialised concurrently
func TestGatewayClient_Initialise_Concurrent(t *testing.T) {
	t.Skip("Concurrent DTLS handshakes fail")

	cert, _ := publicKeyCertificate(testGenerateSigner())
	addr, cancel, err := testCOAPServer{config: dtlsServerConfig(cert), mux: coap.DefaultServeMux}.Start()
	defer cancel()
	if err != nil {
		t.Fatal(err)
	}

	errGroup, _ := errgroup.WithContext(context.Background())
	const num = 5
	for i := 0; i < num; i++ {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		client := &GatewayClient{
			Address: addr,
			Key:     key,
		}
		errGroup.Go(func() error {
			return client.initialise()
		})
	}
	err = errGroup.Wait()
	if err != nil {
		t.Fatal(err)
	}
}

func testGatewayClientAuthenticate(client *GatewayClient, server *testCOAPServer) (err error) {
	if server != nil {
		var cancel func()
		client.Address, cancel, err = server.Start()
		if err != nil {
			panic(err)
		}
		defer cancel()
	}

	err = client.initialise()
	if err != nil {
		return err
	}
	_, err = client.authenticate(authenticatePayload{})
	return err
}

func TestGatewayClient_Authenticate(t *testing.T) {
	info := authenticatePayload{
		TokenId: "12345",
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	cert, _ := publicKeyCertificate(testGenerateSigner())

	tests := []struct {
		name       string
		successful bool
		client     *GatewayClient
		server     *testCOAPServer
	}{
		{name: "success", successful: true, client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAuthCOAPMux(codes.Valid, b)}},
		{name: "unexpected-code", client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAuthCOAPMux(codes.BadGateway, b)}},
		{name: "invalid-auth-payload", client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAuthCOAPMux(codes.Content, []byte("aaaa"))}},
	}
	for _, subtest := range tests {
		t.Run(subtest.name, func(t *testing.T) {
			err := testGatewayClientAuthenticate(subtest.client, subtest.server)
			if subtest.successful && err != nil {
				t.Error(err)
			}
			if !subtest.successful && err == nil {
				t.Error("Expected an error")
			}
		})
	}
}

func testGatewayClientAMInfo(client *GatewayClient, server *testCOAPServer) (err error) {
	if server != nil {
		var cancel func()
		client.Address, cancel, err = server.Start()
		if err != nil {
			panic(err)
		}
		defer cancel()
	}

	err = client.initialise()
	if err != nil {
		return err
	}
	_, err = client.amInfo()
	return err
}

func TestGatewayClient_AMInfo(t *testing.T) {
	info := amInfoSet{
		AccessTokenURL: "/things",
		ThingsVersion:  "1",
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	cert, _ := publicKeyCertificate(testGenerateSigner())

	tests := []struct {
		name       string
		successful bool
		client     *GatewayClient
		server     *testCOAPServer
	}{
		{name: "success", successful: true, client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAMInfoCOAPMux(codes.Content, b)}},
		{name: "unexpected-code", client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAMInfoCOAPMux(codes.BadGateway, b)}},
		{name: "invalid-info", client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAMInfoCOAPMux(codes.Content, []byte("aaaa"))}},
	}
	for _, subtest := range tests {
		t.Run(subtest.name, func(t *testing.T) {
			err := testGatewayClientAMInfo(subtest.client, subtest.server)
			if subtest.successful && err != nil {
				t.Error(err)
			}
			if !subtest.successful && err == nil {
				t.Error("Expected an error")
			}
		})
	}
}

func testGatewayClientAccessToken(client *GatewayClient, server *testCOAPServer) (err error) {
	if server != nil {
		var cancel func()
		client.Address, cancel, err = server.Start()
		if err != nil {
			panic(err)
		}
		defer cancel()
	}

	err = client.initialise()
	if err != nil {
		return err
	}
	_, err = client.accessToken("token", applicationJOSE, "signedWT")
	return err
}

func TestGatewayClient_AccessToken(t *testing.T) {
	cert, _ := publicKeyCertificate(testGenerateSigner())

	tests := []struct {
		name       string
		successful bool
		client     *GatewayClient
		server     *testCOAPServer
	}{
		{name: "success", successful: true, client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAccessTokenCOAPMux(codes.Changed, []byte("{}"))}},
		{name: "unexpected-code", client: &GatewayClient{Key: testGenerateSigner()},
			server: &testCOAPServer{config: dtlsServerConfig(cert), mux: testAccessTokenCOAPMux(codes.BadGateway, []byte("{}"))}},
	}
	for _, subtest := range tests {
		t.Run(subtest.name, func(t *testing.T) {
			err := testGatewayClientAccessToken(subtest.client, subtest.server)
			if subtest.successful && err != nil {
				t.Error(err)
			}
			if !subtest.successful && err == nil {
				t.Error("Expected an error")
			}
		})
	}
}
