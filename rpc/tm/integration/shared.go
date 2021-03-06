// +build integration

// Space above here matters
// Copyright 2017 Monax Industries Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package integration

import (
	"bytes"
	"hash/fnv"
	"testing"

	acm "github.com/hyperledger/burrow/account"
	"github.com/hyperledger/burrow/binary"
	"github.com/hyperledger/burrow/core/integration"
	"github.com/hyperledger/burrow/execution"
	"github.com/hyperledger/burrow/rpc"
	tm_client "github.com/hyperledger/burrow/rpc/tm/client"
	"github.com/hyperledger/burrow/txs"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/rpc/lib/client"
)

const (
	rpcAddr           = "0.0.0.0:46657"
	websocketAddr     = rpcAddr
	websocketEndpoint = "/websocket"
)

// global variables for use across all tests
var (
	privateAccounts = integration.MakePrivateAccounts(5) // make keys
	jsonRpcClient   = rpcclient.NewJSONRPCClient(rpcAddr)
	httpClient      = rpcclient.NewURIClient(rpcAddr)
	clients         = map[string]tm_client.RPCClient{
		"JSONRPC": jsonRpcClient,
		"HTTP":    httpClient,
	}
	genesisDoc = integration.TestGenesisDoc(privateAccounts)
)

//-------------------------------------------------------------------------------
// some default transaction functions

func makeDefaultSendTx(t *testing.T, client tm_client.RPCClient, addr acm.Address, amt uint64) *txs.SendTx {
	sequence := getSequence(t, client, privateAccounts[0].Address())
	tx := txs.NewSendTx()
	tx.AddInputWithSequence(privateAccounts[0].PublicKey(), amt, sequence+1)
	tx.AddOutput(addr, amt)
	return tx
}

func makeDefaultSendTxSigned(t *testing.T, client tm_client.RPCClient, addr acm.Address, amt uint64) *txs.SendTx {
	tx := makeDefaultSendTx(t, client, addr, amt)
	require.NoError(t, tx.Sign(genesisDoc.ChainID(), privateAccounts[0]))
	return tx
}

func makeDefaultCallTx(t *testing.T, client tm_client.RPCClient, addr *acm.Address, code []byte, amt, gasLim,
	fee uint64) *txs.CallTx {
	sequence := getSequence(t, client, privateAccounts[0].Address())
	tx := txs.NewCallTxWithSequence(privateAccounts[0].PublicKey(), addr, code, amt, gasLim, fee,
		sequence+1)
	require.NoError(t, tx.Sign(genesisDoc.ChainID(), privateAccounts[0]))
	return tx
}

func makeDefaultNameTx(t *testing.T, client tm_client.RPCClient, name, value string, amt, fee uint64) *txs.NameTx {
	sequence := getSequence(t, client, privateAccounts[0].Address())
	tx := txs.NewNameTxWithSequence(privateAccounts[0].PublicKey(), name, value, amt, fee, sequence+1)
	require.NoError(t, tx.Sign(genesisDoc.ChainID(), privateAccounts[0]))
	return tx
}

//-------------------------------------------------------------------------------
// rpc call wrappers (fail on err)

// get an account's sequence number
func getSequence(t *testing.T, client tm_client.RPCClient, addr acm.Address) uint64 {
	acc, err := tm_client.GetAccount(client, addr)
	if err != nil {
		t.Fatal(err)
	}
	if acc == nil {
		return 0
	}
	return acc.Sequence()
}

// get the account
func getAccount(t *testing.T, client tm_client.RPCClient, addr acm.Address) acm.Account {
	ac, err := tm_client.GetAccount(client, addr)
	if err != nil {
		t.Fatal(err)
	}
	return ac
}

// sign transaction
func signTx(t *testing.T, client tm_client.RPCClient, tx txs.Tx,
	privAcc *acm.ConcretePrivateAccount) txs.Tx {
	signedTx, err := tm_client.SignTx(client, tx, []*acm.ConcretePrivateAccount{privAcc})
	if err != nil {
		t.Fatal(err)
	}
	return signedTx
}

// broadcast transaction
func broadcastTx(t *testing.T, client tm_client.RPCClient, tx txs.Tx) *txs.Receipt {
	rec, err := tm_client.BroadcastTx(client, tx)
	require.NoError(t, err)
	return rec
}

// dump all storage for an account. currently unused
func dumpStorage(t *testing.T, addr acm.Address) *rpc.ResultDumpStorage {
	client := clients["HTTP"]
	resp, err := tm_client.DumpStorage(client, addr)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func getStorage(t *testing.T, client tm_client.RPCClient, addr acm.Address, key []byte) []byte {
	resp, err := tm_client.GetStorage(client, addr, key)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func callCode(t *testing.T, client tm_client.RPCClient, fromAddress acm.Address, code, data,
	expected []byte) {
	resp, err := tm_client.CallCode(client, fromAddress, code, data)
	if err != nil {
		t.Fatal(err)
	}
	ret := resp.Return
	// NOTE: we don't flip memory when it comes out of RETURN (?!)
	if bytes.Compare(ret, binary.LeftPadWord256(expected).Bytes()) != 0 {
		t.Fatalf("Conflicting return value. Got %x, expected %x", ret, expected)
	}
}

func callContract(t *testing.T, client tm_client.RPCClient, fromAddress, toAddress acm.Address,
	data, expected []byte) {
	resp, err := tm_client.Call(client, fromAddress, toAddress, data)
	if err != nil {
		t.Fatal(err)
	}
	ret := resp.Return
	// NOTE: we don't flip memory when it comes out of RETURN (?!)
	if bytes.Compare(ret, binary.LeftPadWord256(expected).Bytes()) != 0 {
		t.Fatalf("Conflicting return value. Got %x, expected %x", ret, expected)
	}
}

// get the namereg entry
func getNameRegEntry(t *testing.T, client tm_client.RPCClient, name string) *execution.NameRegEntry {
	entry, err := tm_client.GetName(client, name)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

// Returns a positive int64 hash of text (consumers want int64 instead of uint64)
func hashString(text string) uint64 {
	hasher := fnv.New64()
	hasher.Write([]byte(text))
	return uint64(hasher.Sum64())
}

//--------------------------------------------------------------------------------
// utility verification function

// simple contract returns 5 + 6 = 0xb
func simpleContract() ([]byte, []byte, []byte) {
	// this is the code we want to run when the contract is called
	contractCode := []byte{0x60, 0x5, 0x60, 0x6, 0x1, 0x60, 0x0, 0x52, 0x60, 0x20,
		0x60, 0x0, 0xf3}
	// the is the code we need to return the contractCode when the contract is initialized
	lenCode := len(contractCode)
	// push code to the stack
	//code := append([]byte{byte(0x60 + lenCode - 1)}, RightPadWord256(contractCode).Bytes()...)
	code := append([]byte{0x7f},
		binary.RightPadWord256(contractCode).Bytes()...)
	// store it in memory
	code = append(code, []byte{0x60, 0x0, 0x52}...)
	// return whats in memory
	//code = append(code, []byte{0x60, byte(32 - lenCode), 0x60, byte(lenCode), 0xf3}...)
	code = append(code, []byte{0x60, byte(lenCode), 0x60, 0x0, 0xf3}...)
	// return init code, contract code, expected return
	return code, contractCode, binary.LeftPadBytes([]byte{0xb}, 32)
}

// simple call contract calls another contract
func simpleCallContract(addr acm.Address) ([]byte, []byte, []byte) {
	gas1, gas2 := byte(0x1), byte(0x1)
	value := byte(0x1)
	inOff, inSize := byte(0x0), byte(0x0) // no call data
	retOff, retSize := byte(0x0), byte(0x20)
	// this is the code we want to run (call a contract and return)
	contractCode := []byte{0x60, retSize, 0x60, retOff, 0x60, inSize, 0x60, inOff,
		0x60, value, 0x73}
	contractCode = append(contractCode, addr.Bytes()...)
	contractCode = append(contractCode, []byte{0x61, gas1, gas2, 0xf1, 0x60, 0x20,
		0x60, 0x0, 0xf3}...)

	// the is the code we need to return; the contractCode when the contract is initialized
	// it should copy the code from the input into memory
	lenCode := len(contractCode)
	memOff := byte(0x0)
	inOff = byte(0xc) // length of code before codeContract
	length := byte(lenCode)

	code := []byte{0x60, length, 0x60, inOff, 0x60, memOff, 0x37}
	// return whats in memory
	code = append(code, []byte{0x60, byte(lenCode), 0x60, 0x0, 0xf3}...)
	code = append(code, contractCode...)
	// return init code, contract code, expected return
	return code, contractCode, binary.LeftPadBytes([]byte{0xb}, 32)
}
