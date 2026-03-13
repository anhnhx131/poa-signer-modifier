#!/bin/sh

exec geth --datadir /data/ethereum \
  --networkid 2130700783 \
  --gcmode="archive" \
  --http \
  --http.api="clique,personal,eth,net,web3,miner,admin" \
  --http.corsdomain="*" \
  --http.vhosts="*" \
  --http.addr="0.0.0.0" \
  --http.port=8545 \
  --snapshot=false \
  --syncmode="full" \
  --mine \
  --miner.etherbase=0x42Bc433008497fe40c17828F4b7e8e5d781c320e \
  --unlock 0x42Bc433008497fe40c17828F4b7e8e5d781c320e \
  --password /data/ethereum/password.txt \
  --allow-insecure-unlock \
  --rpc.allow-unprotected-txs
