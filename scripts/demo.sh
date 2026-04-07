#!/usr/bin/env sh

set -eu

API_URL="${API_URL:-http://localhost:8080}"

echo "Accounts:"
curl -s "$API_URL/accounts"
echo
echo

echo "Successful transfer:"
curl -s -X POST "$API_URL/transactions" \
  -H "Content-Type: application/json" \
  -d '{"source_account_id":1,"destination_account_id":2,"amount":250.00,"currency":"USD"}'
echo
echo

echo "Insufficient funds:"
curl -s -X POST "$API_URL/transactions" \
  -H "Content-Type: application/json" \
  -d '{"source_account_id":4,"destination_account_id":1,"amount":5000.00,"currency":"USD"}'
echo
echo

echo "Large transfer:"
curl -s -X POST "$API_URL/transactions" \
  -H "Content-Type: application/json" \
  -d '{"source_account_id":1,"destination_account_id":3,"amount":7000.00,"currency":"USD"}'
echo
echo

echo "Rapid repeated transfers:"
curl -s -X POST "$API_URL/transactions" \
  -H "Content-Type: application/json" \
  -d '{"source_account_id":2,"destination_account_id":3,"amount":50.00,"currency":"USD"}'
echo
curl -s -X POST "$API_URL/transactions" \
  -H "Content-Type: application/json" \
  -d '{"source_account_id":2,"destination_account_id":3,"amount":55.00,"currency":"USD"}'
echo
curl -s -X POST "$API_URL/transactions" \
  -H "Content-Type: application/json" \
  -d '{"source_account_id":2,"destination_account_id":3,"amount":60.00,"currency":"USD"}'
echo
echo

echo "Alerts:"
curl -s "$API_URL/alerts"
echo
