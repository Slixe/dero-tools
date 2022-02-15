package main

import (
	"encoding/hex"
	"fmt"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/walletapi"
	"github.com/docopt/docopt-go"
	"math/rand"
	"os"
	"strconv"
)

var cmd = `DERO Simulator for DERO HE by Slixe
DERO : A secure, private blockchain with smart-contracts

Usage:
  simulator [options] 
  simulator -h | --help

Options:
  --debug				    Enable debug
  --accounts=<amount>		Amount of accounts generated
  --daemon-address=<host:port> Use daemon instance at <host>:<port> or https://domain
`

type Output struct {
	wallet *walletapi.Wallet_Memory // necessary to build a tx
	address string
	seed string // seed encoded in hex
	registration string // tx registration encoded in hex
	tx string // tx with amount & receiver encoded in hex
	amount int // atomic unit
	receiver string
}

const fileName = "output.csv"

var debug = false
var accounts = 50
func main() {
	Arguments, err := docopt.Parse(cmd, nil, true, "DERO Stress test : work in progress", false)
	if err != nil {
		fmt.Println("Error while parsing options err:", err)
	}
	globals.Arguments = Arguments
	globals.Initialize()

	if value := globals.Arguments["--debug"]; value != nil {
		debug = value.(bool)
	}

	if value := globals.Arguments["--accounts"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil || intValue < 2 {
			fmt.Println("Invalid number for accounts parameter")
			return
		}
		accounts = intValue
	}
	walletapi.Initialize_LookupTable(1, 1<<20)
	go walletapi.Keep_Connectivity()

	fmt.Println("Debug:", debug)
	fmt.Println("Accounts:", accounts)

	_, err = os.Stat(fileName)
	onDisk := !os.IsNotExist(err)
	var file *os.File
	if onDisk {
		file, err = os.Open(fileName)
	} else {
		file, err = os.Create(fileName)
	}

	if err != nil {
		panic(err)
	}

	var outputs []Output
	for i := 0; i < accounts; i++ {
		w, err := walletapi.Create_Encrypted_Wallet_Random_Memory("")
		if err != nil {
			panic(err)
		}
		output := Output{
			wallet: w,
			address:      w.GetAddress().String(),
			seed:         hex.EncodeToString([]byte(w.GetSeed())),
			registration: hex.EncodeToString(w.GetRegistrationTX().Serialize()),
			tx:           "",
			amount:       rand.Intn(7 * 100000), // max 7 DERO
			receiver:     "",
		}

		outputs = append(outputs, output)
	}

	for _, output := range outputs {
		for {
			v := outputs[rand.Intn(len(outputs)) - 1]
			if v.address != output.address {
				output.receiver = v.address
				break
			}
		}

		tx, err := output.wallet.TransferPayload0([]rpc.Transfer{{Amount: uint64(output.amount), Destination: output.receiver, Payload_RPC: rpc.Arguments{}}}, 4, false, rpc.Arguments{}, 0, false)
		if err != nil {
			panic(err)
		}

		output.tx = hex.EncodeToString(tx.Serialize())
		file.WriteString(output.String())
		file.WriteString("\n")
	}

	file.Sync()
	file.Close()
}

//format: SenderwalletAddress,seed,tgReg,TXiD,AMT,ReceiverWalletAddress.
func (o Output) String() string {
	return fmt.Sprintf("%s,%s,%s,%s,%d,%s", o.address, o.seed, o.registration, o.tx, o.amount, o.receiver)
}