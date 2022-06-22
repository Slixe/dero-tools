package main

import (
	"encoding/hex"
	"fmt"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/transaction"
	"github.com/deroproject/derohe/walletapi"
	"github.com/deroproject/derohe/walletapi/mnemonics"
	"github.com/docopt/docopt-go"
	"runtime"
	"strconv"
)

var cmd = `DERO ColdWallet by Slixe
DERO : A secure, private blockchain with smart-contracts

Usage:
  coldwallet [options] 
  coldwallet -h | --help

Options:
  --languages						Show languages available
  --seed=<seed>						Seed
  --language=<id>					Seed language
`
var language mnemonics.Language

func main() {
	arguments, err := docopt.Parse(cmd, nil, true, "DERO ColdWallet", false)
	if err != nil {
		fmt.Println("Error while parsing options err:", err)
	}

	globals.Arguments = arguments
	globals.Arguments["--debug"] = false
	globals.Arguments["--testnet"] = false
	globals.Initialize()

	if value := globals.Arguments["--languages"]; value != nil {
		for i, l := range mnemonics.Language_List() {
			fmt.Printf("(%d): %s\n", i, l)
		}
		return
	}

	if value := globals.Arguments["--language"]; value != nil {
		if s, err := strconv.Atoi(value.(string)); err == nil && s >= 0 && s < len(mnemonics.Languages) {
			language = mnemonics.Languages[s]
			fmt.Println("Language seed selected:", language.Name)
		} else {
			fmt.Println("Invalid choice, please select a valid language.")
			return
		}
	} else {
		fmt.Println("No language for seed selected!")
		fmt.Println("Available:")
		for i, l := range mnemonics.Language_List() {
			fmt.Printf("(%d): %s\n", i, l)
		}
		return
	}

	fmt.Println("Version:", config.Version)
	fmt.Println("Generating new random wallet...")
	w, err := walletapi.Create_Encrypted_Wallet_Random_Memory("")
	if err != nil {
		fmt.Println("Error while generating random wallet:", err)
		return
	}
	w.SetOfflineMode()
	w.SetNetwork(true)

	fmt.Println("DERO Address:", w.GetAddress())
	fmt.Println("SEED:", w.GetSeedinLanguage(language.Name))
	fmt.Println("Generating valid TX Registration... This can take up to 2hours!")

	txChan := make(chan *transaction.Transaction)
	counter := 0
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		go func() {
			for counter == 0 {
				tx := w.GetRegistrationTX()
				hash := tx.GetHash()
				if hash[0] == 0 && hash[1] == 0 && hash[2] == 0 {
					txChan <- tx
					counter++
					break
				}
			}
		}()
	}
	regTx := <- txChan
	fmt.Println("Tx Registration hex:", hex.EncodeToString(regTx.Serialize()))
	fmt.Println("you must propagate the registration transaction yourself (through SendRawTransaction API) for your account to be valid and registered on the blockchain!")
}
