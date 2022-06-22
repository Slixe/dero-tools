package main

import (
	"bufio"
	"fmt"
	"github.com/deroproject/derosuite/globals"
	"github.com/deroproject/derosuite/walletapi"
	"github.com/deroproject/derosuite/walletapi/mnemonics"
	"github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

var reader = bufio.NewReader(os.Stdin)

func main() {
	fmt.Println("DERO Bruteforce: Your seed does not contain the 24 (or 25) words? This program allows you to find your wallet by bruteforce method until the DERO address matches the one generated")
	globals.Arguments = map[string]interface{}{}
	globals.Arguments["--debug"] = false
	globals.Arguments["--testnet"] = false
	globals.Initialize()
	globals.Logger.Out = ioutil.Discard
	logrus.SetOutput(ioutil.Discard)

	fmt.Printf("Expected DERO Address: ")
	strAddress := readInput()
	addr, err := globals.ParseValidateAddress(strAddress)
	if err != nil {
		panic(fmt.Sprintf("An error has occured while parsing dero address: %s", err))
	}

	languages := mnemonics.Language_List()
	fmt.Println("Language list for seeds:")
	for i := range languages {
		fmt.Printf("[%d] %s\n", i, languages[i])
	}
	fmt.Printf("Please enter a number: ")
	languageChoice := 0 // 0 for english
	if s, err := strconv.Atoi(readInput()); err == nil && s >= 0 && s <= len(languages) {
		languageChoice = s
	} else {
		panic("Invalid choice, please select a valid language.")
	}

	language := mnemonics.Languages[languageChoice]

	fmt.Printf("Seed: ")
	seed := readInput()
	fmt.Println("Validating seed...")
	words := strings.Split(seed, " ")
	for i, seedWord := range words {
		found := false
		for _, word := range language.Words {
			if seedWord == word {
				found = true
				break
			}
		}

		if !found {
			fmt.Printf("Invalid seed word '%s' at position %d\n", seedWord, i)
			return
		}
	}
	fmt.Println("Seed have valid words.")
	fmt.Printf("Seed contains %d words.\n", len(words))
	if len(words) > 25 {
		fmt.Println("Invalid seed, max 25 words allowed.")
		return
	}
	if len(words) == 25 {
		fmt.Println("Your seed contains already 25 words.")
		return
	}

	missingWords := 25 - len(words)
	fmt.Printf("Missing %d words, trying to bruteforce!\n", missingWords)
	//for {
	//for i := 0; i < missing_words; i++ { //TODO, yet we're only working on one word missing
	for pos := 0; pos < 25; pos++ {
		for _, word := range language.Words {
			completeSeed := append(words, "")
			copy(completeSeed[pos+1:], completeSeed[pos:])
			completeSeed[pos] = word

			//fmt.Printf("Testing word '%s' at '%d' position, seed: '%s'\n", word, pos, strings.Join(complete_seed, " "))
			w, err := walletapi.Generate_Account_From_Recovery_Words(strings.Join(completeSeed, " "))
			if err == nil && w != nil && w.GetAddress().String() == addr.String() {
				fmt.Printf("FOUND!!! Missing word is '%s' at position '%d'\n", word, pos)
				fmt.Println("Valid seed is:", strings.Join(completeSeed, " "))
				return
			}
		}
	}
	//}

	//}
}

func readInput() string {
	input, err := reader.ReadString('\n')
	if err != nil {
		panic(fmt.Sprintf("Error while reading user input: %s", err))
	}

	return strings.TrimSuffix(strings.TrimSpace(input), "\n")
}