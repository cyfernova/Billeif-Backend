#!/bin/bash
set -e

echo "Checking Docker..."
docker --version || { echo "Docker not installed"; exit 1; }

echo "Checking Docker Compose..."
docker-compose --version || { echo "Docker Compose not installed"; exit 1; }

echo "Installing tflocal..."
pip3 install --quiet tflocal || pip install --quiet tflocal

echo "Installing awslocal..."
pip3 install --quiet awscli-local || pip install --quiet awscli-local

echo "Installing migrate..."
curl -L https://github.com/golang-migrate/migrate/releases/download/v4.18.1/migrate.linux-amd64.tar.gz | tar xvz
sudo mv migrate /usr/local/bin/migrate

echo "Setup complete!"
