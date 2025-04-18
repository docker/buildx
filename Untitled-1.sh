#!/bin/bash

# Step 1: Update Homebrew
echo "🔧 Updating Homebrew..."
brew update

# Step 2: Install Node.js and npm
echo "🔧 Installing Node.js..."
brew install node

# Step 3: Install Python
echo "🔧 Installing Python 3..."
brew install python

# Step 4: Install Docker
echo "🔧 Installing Docker..."
brew install --cask docker

# Step 5: Install Visual Studio Code
echo "🔧 Installing VS Code..."
brew install --cask visual-studio-code

# Step 6: Install Git and GitHub CLI
echo "🔧 Installing Git and GitHub CLI..."
brew install git
brew install gh

# Step 7: Install Go (optional for node engine)
echo "🔧 Installing GoLang..."
brew install go

# Step 8: Install Rust (optional for performance-critical modules)
echo "🔧 Installing Rust..."
if ! command -v rustc &>/dev/null; then
    if curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y; then
        echo "✅ Rust installed successfully."
    else
        echo "❌ Failed to install Rust. Please check the installation logs for more details."
        exit 1
    fi
else
    echo "✅ Rust is already installed."
fi

# Step 9: Install Hardhat
echo "🔧 Setting up Hardhat..."
mkdir MHD-Blockchain
cd MHD-Blockchain
npm init -y
npm install --save-dev hardhat
npx hardhat --version

# Step 10: Create Hardhat Project
npx hardhat init

echo "✅ MHD Blockchain environment setup is complete!"
echo "📁 Project directory: MHD-Blockchain"
echo "💡 To start working, run: cd MHD-Blockchain && code ."