#!/bin/bash

# This script sends test events to the API Service (Ingestion Service).
# Ensure that the API service is running on localhost:8080 before running this script.
# usage: ./test_script.sh

echo "Sending 'registration' event to API Service..."
curl -v -X POST http://localhost:8080/events \
    -H "Content-Type: application/json" \
    -d '{
        "type": "registration",
        "data": {
            "user_id": "12345",
            "email": "test@example.com",
            "timestamp": "2023-10-27T10:00:00Z"
        }
    }'

echo -e "\n\nSending 'payment' event to API Service..."
curl -v -X POST http://localhost:8080/events \
    -H "Content-Type: application/json" \
    -d '{
        "type": "payment",
        "data": {
            "amount": 100,
            "currency": "USD",
            "timestamp": "2023-10-27T10:05:00Z"
        }
    }'

echo -e "\n\nDone. Check the Worker logs to see if the events were processed."
