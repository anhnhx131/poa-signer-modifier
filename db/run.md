- Import
```bash
docker run --rm -v ./:/data/ethereum ethereum/client-go:v1.11.6 account import --datadir=/data/ethereum --password /data/ethereum/password.txt /data/ethereum/key.prv
```
- Init
```bash
docker run --rm -v ./:/data/ethereum ethereum/client-go:v1.11.6 init --datadir=/data/ethereum /data/ethereum/genesis.json     
```
- Run
```bash
docker run --name joc-mainnet -v ./:/data/ethereum -d -p 8545:8545 --restart="unless-stopped" --entrypoint /data/ethereum/entrypoint.sh ethereum/client-go:v1.11.6
```