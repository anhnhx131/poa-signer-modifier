package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/clique"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
)

const checkpointInterval = 1024

// snapshotData is used to read/write JSON snapshot (only fields stored in DB).
type snapshotData struct {
	Number  uint64                          `json:"number"`
	Hash    common.Hash                     `json:"hash"`
	Signers map[common.Address]struct{}     `json:"signers"`
	Recents map[uint64]common.Address       `json:"recents"`
	Votes   []*clique.Vote                  `json:"votes"`
	Tally   map[common.Address]clique.Tally `json:"tally"`
}

func main() {
	basePath := flag.String("db", "", "Base path to geth data dir (chaindata is appended automatically); e.g. /data/ethereum/geth or ../geth")
	flag.Parse()

	if *basePath == "" {
		fmt.Fprintln(os.Stderr, "Error: -db is required. Example: -db /data/ethereum/geth")
		flag.Usage()
		os.Exit(1)
	}

	dbPath := filepath.Join(*basePath, "chaindata")

	needWrite := len(flag.Args()) > 0 && (flag.Arg(0) == "sethead" ||
		(flag.Arg(0) == "signers" && len(flag.Args()) >= 2 &&
			(flag.Arg(1) == "set" || flag.Arg(1) == "add" || flag.Arg(1) == "remove")))
	readonly := !needWrite
	db, err := rawdb.NewLevelDBDatabase(dbPath, 0, 0, "", readonly)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Open DB: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	args := flag.Args()
	if len(args) == 0 {
		printBlock(db)
		return
	}

	switch args[0] {
	case "block":
		printBlock(db)
	case "sethead":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: sethead <block_number> (e.g. sethead 11264)")
			os.Exit(1)
		}
		setHead(db, args[1])
	case "signers":
		if len(args) < 2 {
			printSigners(db)
			return
		}
		switch args[1] {
		case "list":
			printSigners(db)
		case "set":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "Usage: signers set <addr1> [addr2 ...]")
				os.Exit(1)
			}
			setSigners(db, args[2:])
		case "add":
			if len(args) != 3 {
				fmt.Fprintln(os.Stderr, "Usage: signers add <addr>")
				os.Exit(1)
			}
			addSigner(db, args[2])
		case "remove":
			if len(args) != 3 {
				fmt.Fprintln(os.Stderr, "Usage: signers remove <addr>")
				os.Exit(1)
			}
			removeSigner(db, args[2])
		default:
			fmt.Fprintf(os.Stderr, "Invalid signers command: %q (use: list | set | add | remove)\n", args[1])
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Invalid command: %q (use: block | sethead | signers)\n", args[0])
		os.Exit(1)
	}
}

// setHead sets the chain head to the specified block (truncates chain, node will treat that block as tip).
// Use when forking from a checkpoint: modify signers at checkpoint then sethead to that block number.
func setHead(db ethdb.Database, numStr string) {
	num, err := strconv.ParseUint(numStr, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid block number: %q\n", numStr)
		os.Exit(1)
	}
	hash := rawdb.ReadCanonicalHash(db, num)
	if hash == (common.Hash{}) {
		fmt.Fprintf(os.Stderr, "No canonical block at number %d.\n", num)
		os.Exit(1)
	}
	header := rawdb.ReadHeader(db, hash, num)
	if header == nil {
		fmt.Fprintf(os.Stderr, "Could not read header for block %d (%s).\n", num, hash.Hex())
		os.Exit(1)
	}
	rawdb.WriteHeadHeaderHash(db, hash)
	rawdb.WriteHeadBlockHash(db, hash)
	rawdb.WriteHeadFastBlockHash(db, hash)
	rawdb.WriteFinalizedBlockHash(db, hash)
	fmt.Printf("Head set to block #%d (hash %s).\n", num, hash.Hex())
	fmt.Println("Restart node: chain will start from this block, new blocks will build on top.")
}

func printBlock(db ethdb.Reader) {
	block := rawdb.ReadHeadBlock(db)
	if block == nil {
		fmt.Println("No head block in DB.")
		return
	}
	header := block.Header()
	fmt.Println("--- Latest block (head) ---")
	fmt.Println("Number:", header.Number)
	fmt.Println("Hash:", block.Hash().Hex())
	fmt.Println("ParentHash:", header.ParentHash.Hex())
	fmt.Println("Coinbase:", header.Coinbase.Hex())
	fmt.Println("Difficulty:", header.Difficulty)
	fmt.Println("Time:", header.Time)
	fmt.Println("Extra length:", len(header.Extra))
}

func getCheckpointSnapshot(db ethdb.Reader) (*snapshotData, common.Hash, uint64, bool) {
	headHash := rawdb.ReadHeadBlockHash(db)
	if headHash == (common.Hash{}) {
		return nil, common.Hash{}, 0, false
	}
	headNum := rawdb.ReadHeaderNumber(db, headHash)
	if headNum == nil {
		return nil, common.Hash{}, 0, false
	}
	num := *headNum
	// Nearest checkpoint (block number divisible by 1024)
	checkpointNum := (num / checkpointInterval) * checkpointInterval
	if checkpointNum == 0 {
		// Genesis: snapshot may be stored with block 0 hash
		checkpointHash := rawdb.ReadCanonicalHash(db, 0)
		if checkpointHash == (common.Hash{}) {
			return nil, common.Hash{}, 0, false
		}
		blob, err := db.Get(append(rawdb.CliqueSnapshotPrefix, checkpointHash[:]...))
		if err != nil || len(blob) == 0 {
			return nil, common.Hash{}, 0, false
		}
		var snap snapshotData
		if json.Unmarshal(blob, &snap) != nil {
			return nil, common.Hash{}, 0, false
		}
		return &snap, checkpointHash, checkpointNum, true
	}
	checkpointHash := rawdb.ReadCanonicalHash(db, checkpointNum)
	if checkpointHash == (common.Hash{}) {
		return nil, common.Hash{}, 0, false
	}
	blob, err := db.Get(append(rawdb.CliqueSnapshotPrefix, checkpointHash[:]...))
	if err != nil || len(blob) == 0 {
		return nil, common.Hash{}, 0, false
	}
	var snap snapshotData
	if json.Unmarshal(blob, &snap) != nil {
		return nil, common.Hash{}, 0, false
	}
	return &snap, checkpointHash, checkpointNum, true
}

func printSigners(db ethdb.Reader) {
	snap, checkpointHash, checkpointNum, ok := getCheckpointSnapshot(db)
	if !ok {
		fmt.Println("Clique snapshot not found (check if DB is a Clique chain and has a checkpoint).")
		return
	}
	fmt.Printf("--- Snapshot at checkpoint #%d (hash %s) ---\n", checkpointNum, checkpointHash.Hex())
	if snap.Signers == nil {
		snap.Signers = make(map[common.Address]struct{})
	}
	list := make([]common.Address, 0, len(snap.Signers))
	for a := range snap.Signers {
		list = append(list, a)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Hex() < list[j].Hex() })
	fmt.Println("Number of signers:", len(list))
	for _, a := range list {
		fmt.Println(" ", a.Hex())
	}
}

func setSigners(db ethdb.Database, addrs []string) {
	snap, checkpointHash, checkpointNum, ok := getCheckpointSnapshot(db)
	if !ok {
		fmt.Fprintln(os.Stderr, "Clique snapshot not found.")
		os.Exit(1)
	}
	newSigners := make(map[common.Address]struct{})
	for _, s := range addrs {
		if !common.IsHexAddress(s) {
			fmt.Fprintf(os.Stderr, "Invalid address: %q\n", s)
			os.Exit(1)
		}
		newSigners[common.HexToAddress(s)] = struct{}{}
	}
	snap.Signers = newSigners
	// Clear votes/tally for a clean state from checkpoint
	snap.Votes = nil
	snap.Tally = make(map[common.Address]clique.Tally)
	writeSnapshot(db, checkpointHash, snap)
	fmt.Printf("Wrote %d signers to snapshot checkpoint #%d (%s).\n", len(newSigners), checkpointNum, checkpointHash.Hex())
}

func addSigner(db ethdb.Database, addrStr string) {
	if !common.IsHexAddress(addrStr) {
		fmt.Fprintf(os.Stderr, "Invalid address: %q\n", addrStr)
		os.Exit(1)
	}
	addr := common.HexToAddress(addrStr)
	snap, checkpointHash, checkpointNum, ok := getCheckpointSnapshot(db)
	if !ok {
		fmt.Fprintln(os.Stderr, "Clique snapshot not found.")
		os.Exit(1)
	}
	if snap.Signers == nil {
		snap.Signers = make(map[common.Address]struct{})
	}
	snap.Signers[addr] = struct{}{}
	writeSnapshot(db, checkpointHash, snap)
	fmt.Printf("Added %s to snapshot checkpoint #%d.\n", addr.Hex(), checkpointNum)
}

func removeSigner(db ethdb.Database, addrStr string) {
	if !common.IsHexAddress(addrStr) {
		fmt.Fprintf(os.Stderr, "Invalid address: %q\n", addrStr)
		os.Exit(1)
	}
	addr := common.HexToAddress(addrStr)
	snap, checkpointHash, checkpointNum, ok := getCheckpointSnapshot(db)
	if !ok {
		fmt.Fprintln(os.Stderr, "Clique snapshot not found.")
		os.Exit(1)
	}
	delete(snap.Signers, addr)
	writeSnapshot(db, checkpointHash, snap)
	fmt.Printf("Removed %s from snapshot checkpoint #%d.\n", addr.Hex(), checkpointNum)
}

func writeSnapshot(db ethdb.KeyValueWriter, hash common.Hash, snap *snapshotData) {
	blob, err := json.Marshal(snap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Marshal snapshot: %v\n", err)
		os.Exit(1)
	}
	key := append(rawdb.CliqueSnapshotPrefix, hash[:]...)
	if err := db.Put(key, blob); err != nil {
		fmt.Fprintf(os.Stderr, "Write DB: %v\n", err)
		os.Exit(1)
	}
}
