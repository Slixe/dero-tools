package main

import (
	"fmt"
	"github.com/deroproject/derosuite/address"
	"github.com/deroproject/derosuite/globals"
	"github.com/deroproject/derosuite/walletapi"
	"github.com/deroproject/derosuite/walletapi/mnemonics"
	"github.com/docopt/docopt-go"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

var cmd = `DERO Bruteforce for Atlantis version by Slixe
DERO : A secure, private blockchain with smart-contracts

Usage:
  bruteforce [options] 
  bruteforce -h | --help

Options:
  --debug                           Enable debug
  --languages                       Show languages available
  --expected-address=<address>      Expected DERO Address
  --seed=<seed>                     Seed
  --language=<id>                   Seed language
  --mode=<number>                   Available: Missing Words (0), Invalid Order (1)
  --threads=<threads>               Number of threads used
`

type BruteforceMode int

const (
	MissingWord BruteforceMode = iota
	InvalidOrder
)

var threadsAvailable = runtime.GOMAXPROCS(0) / 2
var language mnemonics.Language
var seed string
var addr *address.Address
var mode BruteforceMode
var wg sync.WaitGroup

func main() {
	Arguments, err := docopt.Parse(cmd, nil, true, "DERO Bruteforce", false)
	if err != nil {
		fmt.Println("Error while parsing options err:", err)
	}

	globals.Arguments = Arguments
	globals.Arguments["--debug"] = false
	globals.Arguments["--testnet"] = false
	globals.Initialize()

	if value := globals.Arguments["--languages"]; value != nil {
		for i, l := range mnemonics.Language_List() {
			fmt.Printf("(%d): %s\n", i, l)
		}
		return
	}

	if value := globals.Arguments["--seed"]; value != nil {
		seed = value.(string)
	} else {
		fmt.Println("No seed found!")
		return
	}

	if value := globals.Arguments["--language"]; value != nil {
		if s, err := strconv.Atoi(value.(string)); err == nil && s >= 0 && s < len(mnemonics.Languages) {
			language = mnemonics.Languages[s]
		} else {
			fmt.Println("Invalid choice, please select a valid language.")
			return
		}
	} else {
		fmt.Println("No language code found!")
		return
	}

	if value := globals.Arguments["--expected-address"]; value != nil {
		v, err := globals.ParseValidateAddress(value.(string))
		if err != nil {
			fmt.Printf("An error has occured while parsing dero address: %s\n", err)
			return
		}
		addr = v
	} else {
		fmt.Println("No expected DERO Address found!")
		return
	}

	if value := globals.Arguments["--mode"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil {
			fmt.Println("Error, invalid mode: ", value, err)
			return
		}

		switch intValue {
		case 0:
			mode = MissingWord
			fmt.Println("this mode try to bruteforce last N missing words")
		case 1:
			mode = InvalidOrder
			fmt.Println("This mode shuffle the 24-words seed until the expected dero address is found")
		default:
			fmt.Println("Invalid mode!")
			return
		}
	} else {
		fmt.Println("No mode selected!")
		return
	}

	if value := globals.Arguments["--threads"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil {
			panic(err)
		}

		if intValue < 1 {
			intValue = 1
			fmt.Println("Minimum number of threads is 1!")
			return
		}
		threadsAvailable = intValue
	}

	// verify seed validity (verify that each word exist in language selected)
	words := strings.Split(seed, " ")
	fmt.Printf("Seed contains %d words.\n", len(words))
	if len(words) > 25 {
		fmt.Println("Invalid seed, max 25 words allowed.")
		return
	}

	for i, w := range words {
		found := false
		for _, word := range language.Words {
			if w == word {
				found = true
				break
			}
		}

		if !found {
			fmt.Printf("Invalid seed word '%s' at position %d\n", w, i)
			return
		}
	}
	fmt.Println("Seed have valid words.")
	if len(words) == 25 {
		if !mnemonics.Verify_Checksum(words, language.Unique_Prefix_Length) {
			fmt.Println("Invalid checksum for your 25 words seed. Try re-order.")
		} else {
			fmt.Println("Your seed contains already 25 words and is valid.")
		}
		wallet, err := walletapi.Generate_Account_From_Recovery_Words(seed)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Printf("Address associated to this seed: %s\n", wallet.GetAddress())
		if addr.String() == wallet.GetAddress().String() {
			fmt.Println("Address is the same as expected!")
		}

		return
	}

	wg.Add(threadsAvailable)
	for i := 0; i < threadsAvailable; i++ {
		if mode == MissingWord {
			if len(words) > 23 {
				fmt.Println("To use this mode, your seed must have at least one word missing (maximum 23 words)")
				return
			}
			go findMissingWords(words, i)
		} else if mode == InvalidOrder {
			fmt.Println("WIP")
		}
	}
	wg.Wait()
}

func findMissingWords(words []string, thread int) {
	missingWords := 24 - len(words)
	for i := 0; i < missingWords; i++{
		for pos := i; pos < 24; pos++ {
			for _, word := range language.Words {
				completeSeed := append(words, "")
				copy(completeSeed[pos+1:], completeSeed[pos:])
				completeSeed[pos] = word

				fmt.Printf("Testing word '%s' at '%d' position, seed: '%s'\n", word, pos, strings.Join(completeSeed, " "))
				w, err := walletapi.Generate_Account_From_Recovery_Words(strings.Join(completeSeed, " "))
				if err == nil && w != nil && w.GetAddress().String() == addr.String() {
					fmt.Printf("FOUND!!! Missing word is '%s' at position '%d'\n", word, pos)
					fmt.Println("Valid seed is:", strings.Join(completeSeed, " "))
					done()
					return
				}
			}
		}
	}
}

func insert(a []string, index int, value string) []string {
	if len(a) == index {
		return append(a, value)
	}
	v := append(a[:index+1], a[index:]...)
	v[index] = value
	return v
}

func done() {
	for i := 0; i < threadsAvailable; i++ {
		wg.Done()
	}
}