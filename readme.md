# Vault EC2Auth Agent

This agent is intended to make EC2 authentication against Vault as simple as possible. Simply launch the agent in the
background and anytime you need to access vault, your token is available at `~/.vault-token` which also happens to be
the default location that the `vault` CLI looks for its access token by default! 

## How it works

Upon launch, the agent will immediately attempt to connect to Vault at `https://vault.service.consul:8200` to retrieve 
a token for the requested role.
 
The token is written to `~/.vault-token` and the nonce to `~/.vault-nonce`.

The agent will then block for half of the lease duration before attempting to reauthenticate with Vault using the 
nonce value stored in `~/.vault-nonce`.

## Usage

 Typical usage is: 
 
`vault-ec2auth-agent -role my_role`
 
 Additional options can be seen by running the tool with no parameters.