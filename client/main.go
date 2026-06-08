package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/agnostic-t/peerent/internal/oquic"
)

func main() {
	stunServ := flag.String("stun", "stun.sipnet.ru:3478", "Address of STUN server to use")

	flag.CommandLine.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s --stun STUN_SERVER_ADDR PATH_TO_FILE\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: you need to specify file.")
		fmt.Fprintf(os.Stderr, "Usage: %s --stun STUN_SERVER_ADDR PATH_TO_FILE\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(2)
	}

	// Берем первый позиционный аргумент
	filePath := flag.Arg(0)

	p2pCli, err := oquic.NewPeerNetClient()
	if err != nil {
		log.Fatal(err)
	}

	selfAddr, err := p2pCli.GetPublicAddr(*stunServ)
	if err != nil {
		log.Fatal(err)
	}

	var othersAddrStr string
	fmt.Printf("[Pnet] Self public addr: %s\nenter other's ip:port: ", selfAddr)
	fmt.Scan(&othersAddrStr)

	othersAddr, err := net.ResolveUDPAddr("udp", othersAddrStr)
	if err != nil {
		log.Fatal(err)
	}

	if err := p2pCli.PunchNAT(othersAddr); err != nil {
		log.Fatal(err)
	}

	fmt.Println("[Pnet] NAT punched...")

	var choice string
	fmt.Print("[Pnet] enter mode ([d]ownload/[u]pload): ")
	fmt.Scan(&choice)

	if choice == "d" {
		err = p2pCli.RecvFile(filePath)
	} else if choice == "u" {
		err = p2pCli.SendFile(filePath)
	} else {
		fmt.Printf("[Pnet] Unkonwn mode: \"%s\"\n. Only d and u are supported\n", choice)
	}

	if err != nil {
		fmt.Printf("[Pnet] Failed to perform transaction: %v", err)
	} else {
		fmt.Printf("[Pnet] Transcation completed successfully. Run sha256sum to verify\n")
	}
}
