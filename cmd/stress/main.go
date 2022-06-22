package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/cryptography/crypto"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/transaction"
	"github.com/deroproject/derohe/walletapi"
	"github.com/docopt/docopt-go"
	"github.com/ybbus/jsonrpc"
	"io/ioutil"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var cmd = `DERO Stress Test for DERO HE by Slixe
DERO : A secure, private blockchain with smart-contracts

Usage:
  stress [options] 
  stress -h | --help

Options:
  --debug                   Enable debug
  --testnet                 Enable testnet mode
  --ringsize=<size>         Ring Size for each TX generated
  --total-accounts=<amount> Maximum of generated accounts and TXs sent at same time
  --main-address=<address>  Main DERO address (receiver) of all these TXs
  --daemon-address=<host:port> Use daemon instance at <host>:<port> or https://domain
  --threads=<threads>       Number of threads used
  --use-disk                Load & save wallets on disk
  --registration-only       send registration TX only
  --mode=<0|1|2>            Default set to 0. 0: Wait & send in one time. 1: Send at each tx created. 2: Spam until stopped
  --rounds=<value>          Used by spam mode. How many time you want to repeat the spam
  --amount-transferred=<amount> Amount transferred on each transaction
  --output=<filename>       Record all txs sent to daemon in filename
  --wait-n-blocks=<amount>  Wait N blocks before fetching txs for output option.
  --use-sc-config=<filename> Use configuration to send SC TXs
  --generate-example-sc-config=<filename> Generate an example SC Config
  --repeat-tx=<value>       Send the TX N times to the node
  --skip-pow                Skip Registration TX PoW
  --empty-tx                Send Empty TX
`

type SCConfig struct {
	DeploySC bool `json:"deploySC"`
	ScCode string `json:"scCode"`
	Scid string `json:"scid"` // if deploy SC is true, scid is not used
	EntryPoint string `json:"entryPoint"`
	Arguments rpc.Arguments `json:"arguments"`
}

type StressMode int

const (
	OneTime StressMode = iota
	OnCreation
	Spam
)

var threadsAvailable = runtime.GOMAXPROCS(0) / 2
var defaultTotalTx = 100 //100 * threadsAvailable
var accountsPerThread = defaultTotalTx / threadsAvailable
var useDisk = false
var mainAddress *rpc.Address
var amount = uint64(1)
var generateOnly = false
var ringSize uint64 = 4
var debug = false
var mode = OneTime
var rounds = 1
var waitNblocks = int64(1)
var scConfig SCConfig
var useSC = false
var repeatTX = 1
var skipPOW = false
var emptyTX = false
// TODO Replace fmt.Println with logger

var txSent int32 = 0
var txError int32 = 0
var txTotal int32 = 0

var deploySC = false
var scMutex sync.Mutex

var wgRegistration sync.WaitGroup
var wgAwaitReg sync.WaitGroup
var wgTxSend sync.WaitGroup
var wgSC sync.WaitGroup

var txsFilename string
var txsMutex sync.Mutex
var txs []crypto.Hash

func main() {
	Arguments, err := docopt.Parse(cmd, nil, true, "DERO Stress test", false)
	if err != nil {
		fmt.Println("Error while parsing options err:", err)
	}
	globals.Arguments = Arguments
	walletapi.Initialize_LookupTable(1, 1<<20)
	globals.Initialize()


	if value := globals.Arguments["--generate-example-sc-config"]; value != nil {
		filename := value.(string)
		scConfig = SCConfig{
			DeploySC:   false,
			ScCode:     "",
			Scid:       "0000000000000000000000000000000000000000000000000000000000000001",
			EntryPoint: "Register",
			Arguments:  rpc.Arguments{
				{
					Name:     "name",
					DataType: rpc.DataString,
					Value:    "Slixe",
				},
			},
		}
		bytes, err := json.MarshalIndent(&scConfig, "", "	")
		if err != nil {
			panic(err)
		}

		file, err := os.Create(filename)
		file.Write(bytes)
		file.Sync()
		file.Close()
		fmt.Println("Config template saved to", filename)
		return
	}

	if value := globals.Arguments["--ringsize"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil {
			panic(err)
		}

		if intValue > 0 && crypto.IsPowerOf2(intValue) {
			ringSize = uint64(intValue)
			fmt.Println("Ring size set to", value)
		} else {
			fmt.Println("Ring size isn't a power of 2!")
			return
		}
	}

	if value := globals.Arguments["--debug"]; value != nil {
		debug = value.(bool)
	}

	if value := globals.Arguments["--main-address"]; value != nil {
		mainAddress, err = globals.ParseValidateAddress(value.(string))
		if err != nil {
			panic(err)
		} else {
			fmt.Println("Main address set to:", value.(string))
		}
	} else {
		mainAddress, _ = globals.ParseValidateAddress("deto1qy0ehnqjpr0wxqnknyc66du2fsxyktppkr8m8e6jvplp954klfjz2qqdzcd8p")
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

	if value := globals.Arguments["--total-accounts"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil {
			panic(err)
		}

		if intValue < threadsAvailable {
			intValue = threadsAvailable
		}

		accountsPerThread = intValue / threadsAvailable
		fmt.Println("Account per thread:", accountsPerThread)
	} else {
		accountsPerThread = defaultTotalTx / threadsAvailable
		fmt.Println("Account per thread:", accountsPerThread)
	}

	if value := globals.Arguments["--use-disk"]; value != nil {
		useDisk = value.(bool)
	}

	if value := globals.Arguments["--registration-only"]; value != nil {
		generateOnly = value.(bool)
	}

	if value := globals.Arguments["--rounds"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil {
			fmt.Println("Error, invalid rounds: ", value, err)
			return
		}

		rounds = intValue
	}

	if value := globals.Arguments["--mode"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil {
			fmt.Println("Error, invalid mode: ", value, err)
			return
		}

		switch intValue {
		case 0:
			mode = OneTime
			fmt.Println("this mode waits on all threads to send all pre-generated transactions at the same time")
		case 1:
			mode = OnCreation
			fmt.Println("this mode sends the transaction to the daemon as soon as it is created")
		case 2:
			mode = Spam
			fmt.Println("This mode sends a transaction each time it is created and is executed until you shut down the program")
			fmt.Println("Rounds:", rounds)
		default:
			fmt.Println("Invalid mode!")
			return
		}
	}

	if value := globals.Arguments["--amount-transferred"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil || intValue <= 0 {
			fmt.Println("Error, invalid amount: ", value)
			return
		}

		amount = uint64(intValue)
	}

	if value := globals.Arguments["--output"]; value != nil {
		txsFilename = value.(string)
	}

	if value := globals.Arguments["--wait-n-blocks"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil || intValue <= 0 {
			fmt.Println("Error, invalid wait-n-blocks param:", value)
			return
		}

		waitNblocks = int64(intValue)
	}

	if value := globals.Arguments["--repeat-tx"]; value != nil {
		intValue, err := strconv.Atoi(value.(string))
		if err != nil {
			panic(err)
		}

		if intValue < 1 {
			intValue = 1
			fmt.Println("Minimum number of repeat TX is 1!")
			return
		}
		repeatTX = intValue
	}

	if value := globals.Arguments["--skip-pow"]; value != nil {
		skipPOW = value.(bool)
	}

	if value := globals.Arguments["--empty-tx"]; value != nil {
		emptyTX = value.(bool)
	}

	if value := globals.Arguments["--use-sc-config"]; value != nil {
		filename := value.(string)
		bytes, err := ioutil.ReadFile(filename)
		if err != nil {
			fmt.Println("Error while reading SC configuration file:", err)
			return
		}

		err = json.Unmarshal(bytes, &scConfig)
		if err != nil {
			fmt.Println("Error while parsing SC Configuration:", err)
			return
		}

		if scConfig.DeploySC {
			if scConfig.Scid != "" {
				fmt.Println("Error, you can't deploy a SC and specify a SCID in the same configuration.")
				return
			}

			codeLen := len(scConfig.ScCode)
			if codeLen == 0 {
				fmt.Println("Error, you want to deploy a SC but no Code or filename is provided!")
				return
			} else if codeLen < 16 && strings.Contains(scConfig.ScCode, ".") {
				bytes, err = ioutil.ReadFile(scConfig.ScCode)
				if err != nil {
					fmt.Println("Expected to read a Smart Contract file:", scConfig.ScCode, "but got an error:", err)
					return
				}
				deploySC = scConfig.DeploySC
				scConfig.ScCode = string(bytes)
				fmt.Println("SC Code to deploy:")
				fmt.Println(scConfig.ScCode)
			}
		} else if len(scConfig.Scid) != 64 {
			fmt.Println("Invalid SCID in SC Configuration!")
			return
		} else {
			scConfig.Arguments = append(scConfig.Arguments, rpc.Argument{Name: rpc.SCID, DataType: rpc.DataHash, Value: crypto.HashHexToHash(scConfig.Scid)})
		}

		scConfig.Arguments = append(scConfig.Arguments, rpc.Argument{Name: rpc.SCACTION, DataType: rpc.DataUint64, Value: uint64(rpc.SC_CALL)})
		useSC = true
	}

	fmt.Println("Wallet version:", config.Version)
	fmt.Println("Receiver address:", mainAddress)
	fmt.Println("Threads:", threadsAvailable)
	fmt.Println("Ring Size:", ringSize)
	fmt.Println("Generate accounts only:", generateOnly)
	fmt.Println("Total TX to be generated:", accountsPerThread * threadsAvailable)
	fmt.Println("Load & save wallets on disk:", useDisk)
	fmt.Println("Selected mode:", mode)
	fmt.Println("Amount transferred:", globals.FormatMoney5(amount), "DERO")
	if len(txsFilename) > 0 {
		fmt.Println("Output:", txsFilename)
		fmt.Println("Will wait", waitNblocks, "blocks before fetching txs from daemon")
	}
	fmt.Println("Repeat TX:", repeatTX)
	fmt.Println("Skip POW:", skipPOW)
	fmt.Println("Empty TX:", emptyTX)
	fmt.Println("Use SC Config:", useSC)

	go walletapi.Keep_Connectivity()
	start()
}

func createThreadAccounts(thread int) {
	var accounts []*walletapi.Wallet_Memory
	var wallets [][]byte

	fileName := fmt.Sprintf("thread#%d-%s.bin", thread, config.Version)
	_, err := os.Stat(fileName)
	onDisk :=  !os.IsNotExist(err)

	if useDisk && onDisk {
		f, err := os.Open(fileName)
		if err != nil {
			panic(err)
		}
		data, err := ioutil.ReadAll(f)
		if err != nil {
			panic(err)
		}

		wallets = bytes.Split(data, []byte("\n"))
		fmt.Println("Wallets from disk available for thread", thread, ":", len(wallets) - 1)
		err = f.Close()
		if err != nil {
			fmt.Println("Error while closing file:", err)
			return
		}
	}

	var transactions []*transaction.Transaction
	for i := 0; i < accountsPerThread; i++ {
		var account *walletapi.Wallet_Memory
		var err error

		if onDisk && (len(wallets) - 1) > i {
			account, err = walletapi.Open_Encrypted_Wallet_Memory("", wallets[i])
		} else {
			account, err = walletapi.Create_Encrypted_Wallet_Random_Memory("")
		}
		if err != nil {
			panic(err)
		}

		account.SetRingSize(int(ringSize))
		account.SetNetwork(false)
		account.SetOnlineMode()
		balance, _ := account.Get_Balance()
		if !account.IsRegistered() && (balance == 0 || account.Get_Registration_TopoHeight() == -1) {
			var txReg *transaction.Transaction

			start := time.Now()
			for {
				txReg = account.GetRegistrationTX()
				if !skipPOW {
					hash := txReg.GetHash()
					if hash[0] == 0 && hash[1] == 0 && hash[2] == 0 {
						break
					}
				} else {
					break
				}
			}
			if debug {
				elapsed := time.Since(start)
				fmt.Printf("Thread #%d: TX Registration PoW took %s\n", thread, elapsed)
			}

			if mode == OnCreation || mode == Spam {
				sendTx(account, txReg)
			} else {
				transactions = append(transactions, txReg)
			}
		}
		accounts = append(accounts, account)
	}

	if mode == OneTime {
		wgRegistration.Done()
		fmt.Println("Waiting to send registration TXs")
		wgRegistration.Wait()
		fmt.Println("All threads are ready!")
	}

	lastWallet := accounts[len(accounts) - 1]
	if len(transactions) > 0 {
		fmt.Println("Start sending TX registrations!")
		for _, tx := range transactions {
			sendTx(lastWallet, tx)
		}
	}

	fmt.Println("Thread", thread, "Waiting on wallets to be registered")

	for {
		allOK := true
		current: for _, account := range accounts {
			if !account.IsRegistered() || account.Get_Registration_TopoHeight() == -1 {
				allOK = false
				break current
			}
		}

		if allOK {
			break
		}
		time.Sleep(1 * time.Second)
	}

	currentHeight := lastWallet.Get_Daemon_TopoHeight()
	for currentHeight + 5  >= lastWallet.Get_Daemon_TopoHeight() { // wait on 5 blocks
		time.Sleep(1 * time.Second)
	}

	fmt.Println("All accounts are registered!")

	if useSC {
		scMutex.Lock() // faster thread will deploy SC if necessary
		if deploySC {
			deploySC = false
			scMutex.Unlock()
			arguments := rpc.Arguments{rpc.Argument{Name: rpc.SCACTION, DataType: rpc.DataUint64, Value: uint64(rpc.SC_INSTALL)}, rpc.Argument{Name: rpc.SCCODE, DataType: rpc.DataString, Value: scConfig.ScCode}}
			tx, err := lastWallet.TransferPayload0([]rpc.Transfer{}, 2, false, arguments, 0, false)
			if err != nil {
				panic(err)
			}
			err = lastWallet.SendTransaction(tx)
			if err != nil {
				panic(err)
			}

			scConfig.Scid = tx.GetHash().String()
			fmt.Println("Generated SC ID is:", scConfig.Scid)
			scConfig.Arguments = append(scConfig.Arguments, rpc.Argument{Name: rpc.SCID, DataType: rpc.DataHash, Value: crypto.HashHexToHash(scConfig.Scid)})

			{
				rpcClient := jsonrpc.NewClient(fmt.Sprintf("http://%s/json_rpc", walletapi.Daemon_Endpoint_Active))
				fmt.Println("Waiting for SC to be registered")
				for {
					time.Sleep(2 * time.Second)
					var result rpc.GetTransaction_Result
					err = rpcClient.CallFor(&result, "DERO.GetTransaction", rpc.GetTransaction_Params{
						Tx_Hashes: []string{scConfig.Scid},
					})
					if err != nil {
						panic(err)
					}

					if result.Status == "OK" {
						if len(result.Txs[0].ValidBlock) > 0 {
							fmt.Println("Smart Contract has been uploaded & accepted by network!")
							break
						}
					}
				}
			}
		} else {
			scMutex.Unlock()
		}
	}

	if useSC && scConfig.DeploySC { // wait on all threads
		wgSC.Done()
		wgSC.Wait()
	}

	if mode == OneTime {
		wgAwaitReg.Done()
		fmt.Println("Thread", thread, "waiting on others threads")
		wgAwaitReg.Wait()
	}

	if !generateOnly {
		var transactions []*transaction.Transaction
		if mode == OneTime {
			fmt.Println("All threads are ready! Start creating transactions!")
		}

		lastHeight := lastWallet.Get_Daemon_TopoHeight()
		localRounds := rounds
		generateTxs:
			for _, account := range accounts {
				start := time.Now()
				tx, err := generateTx(account)
				if err != nil {
					atomic.AddInt32(&txError, 1)
					if debug {
						fmt.Println("Thread", thread, "error while creating tx:", err)
					}
				} else {
					if mode == OnCreation || mode == Spam {
						sendTx(account, tx)
					} else {
						transactions = append(transactions, tx)
					}
					if debug {
						elapsed := time.Since(start)
						fmt.Printf("Thread #%d: TX creating took %s\n", thread, elapsed)
					}
				}
			}
			if mode == Spam {
				for lastHeight == lastWallet.Get_Daemon_TopoHeight() { // wait until new block
					time.Sleep(1 * time.Second)
				}
				lastHeight = lastWallet.Get_Daemon_TopoHeight()
			}

		if mode == OneTime {
			wgTxSend.Done()
			wgTxSend.Wait()
			fmt.Println("All threads are ready!")

			fmt.Println("Thread", thread, "have", len(transactions), "txs to send")
			for _, tx := range transactions {
				sendTx(lastWallet, tx)
			}
		} else if mode == Spam {
			if localRounds > 1 {
				localRounds--
				fmt.Println("Rounds left for thread", thread, ":", localRounds)
				goto generateTxs
			}
		}
	}

	if useDisk {
		fmt.Println("Saving wallets of thread", thread, "on disk")

		var f *os.File
		if onDisk {
			f, err = os.Open(fileName)
		} else {
			f, err = os.Create(fileName)
		}
		if err != nil {
			panic(err)
		}

		for _, account := range accounts {
			f.Write(account.Get_Encrypted_Wallet())
			f.Write([]byte("\n"))
			account.Close_Encrypted_Wallet()
		}
		f.Sync()
		f.Close()
	} else {
		for _, account := range accounts {
			account.Close_Encrypted_Wallet()
		}
	}
}

func generateTx(account *walletapi.Wallet_Memory) (tx *transaction.Transaction, err error) {
	if useSC {
		tx, err = account.TransferPayload0([]rpc.Transfer{}, ringSize, false, scConfig.Arguments, 0, false)
	} else if emptyTX {
		tx, err = account.TransferPayload0([]rpc.Transfer{}, ringSize, false, rpc.Arguments{}, 0, false)
	} else {
		tx, err = account.TransferPayload0([]rpc.Transfer{{Amount: amount, Destination: mainAddress.String(), Payload_RPC: rpc.Arguments{}}}, ringSize, false, rpc.Arguments{}, 0, false)
	}
	return
}

func sendTx(wallet *walletapi.Wallet_Memory, tx *transaction.Transaction) {
	atomic.AddInt32(&txTotal, 1)
	for i := 0; i < repeatTX; i++ {
		if debug {
			fmt.Println("TX Size:", len(hex.EncodeToString(tx.Serialize()))) // show hex size
		}
		err := wallet.SendTransaction(tx)
		if err != nil {
			atomic.AddInt32(&txError, 1)
			if debug {
				fmt.Println("Error while sending TX:", err)
			}
		} else {
			atomic.AddInt32(&txSent, 1)
			txsMutex.Lock()
			txs = append(txs, tx.GetHash())
			txsMutex.Unlock()
		}
	}
}

func start() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<- sigs
		showStats()
		os.Exit(0)
	}()

	var accounts sync.WaitGroup
	accounts.Add(threadsAvailable)
	if mode == OneTime {
		wgTxSend.Add(threadsAvailable)
		wgRegistration.Add(threadsAvailable)
		wgAwaitReg.Add(threadsAvailable)
	}

	if useSC && scConfig.DeploySC {
		wgSC.Add(threadsAvailable)
	}

	for i := 0; i < threadsAvailable; i++ {
		go func(thread int) {
			createThreadAccounts(thread)
			accounts.Done()
		}(i)
	}
	accounts.Wait()
	showStats()
}

func showStats() {
	fmt.Println("Finished, Total TX:", txTotal, "TX success:", txSent, "TX errors:", txError)
	if len(txsFilename) > 0 {
		_, err := os.Stat(txsFilename)
		if !os.IsNotExist(err) {
			err := os.Remove(txsFilename)
			if err != nil {
				panic(err)
			}
		}

		file, err := os.Create(txsFilename)
		if err != nil {
			panic(err)
		}

		rpcClient := jsonrpc.NewClient(fmt.Sprintf("http://%s/json_rpc", walletapi.Daemon_Endpoint_Active))
		var txsHashes []string
		for _, tx := range txs {
			txsHashes = append(txsHashes, tx.String())
		}

		fmt.Println("Waiting for", waitNblocks, "blocks before fetching txs!")
		var heightResult rpc.Daemon_GetHeight_Result
		err = rpcClient.CallFor(&heightResult, "DERO.GetHeight")
		if err != nil {
			fmt.Println("error while fecthing current block height", err)
			return
		}
		currentHeight := heightResult.TopoHeight

		for {
			time.Sleep(2 * time.Second)
			err = rpcClient.CallFor(&heightResult, "DERO.GetHeight")
			if err != nil {
				fmt.Println("error while fecthing last block height", err)
				return
			}

			if heightResult.TopoHeight > currentHeight + waitNblocks {
				break
			}
		}

		var result rpc.GetTransaction_Result
		err = rpcClient.CallFor(&result, "DERO.GetTransaction", rpc.GetTransaction_Params{
			Tx_Hashes: txsHashes,
		})
		if err != nil {
			fmt.Println("Error while retrieving txs from daemon:", err)
			return
		}

		totalMined := 0
		for i, tx := range txsHashes {
			data := result.Txs[i]
			if data.Block_Height != -1 {
				totalMined++
			}
			file.WriteString(fmt.Sprintf("%s,%d,%s\n", tx, data.Block_Height, data.ValidBlock))
		}

		file.Sync()
		file.Close()
		fmt.Println("File", txsFilename, "is now available!")
		fmt.Println(fmt.Sprintf("Total TXs mined: %d/%d", totalMined, len(txsHashes)))
	}
}

func (s StressMode) String() string {
	switch s {
	case Spam:
		return "Spam"
	case OneTime:
		return "One Time"
	case OnCreation:
		return "On Creation"
	default:
		return "unknown"
	}
}