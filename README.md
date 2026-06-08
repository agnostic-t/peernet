# Peernet

Simple project to share files P2P

## Usage

Firstly you need to build program:

```sh
# Change path if you wish
go build -o ~/.local/bin/peerent ./client/main.go
```

Than you can start using it:

```sh
$ peerent --stun stun.actionvoip.com:3478 ./large_file.bin
# Follow instructions
```

## NOTICE

Using VPN can break P2P, or if you are using mobile internet it also can cause some troubles in NAT punching
