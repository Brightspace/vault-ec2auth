# Vault EC2Auth Agent

This agent is intended to make EC2 authentication against Vault as simple as possible. Simply launch the agent in the
background and anytime you need to access vault, your token is available at `~/.vault-token` which is the default location 
that the `vault` CLI looks for its access token. 

## How it works

Upon launch, the agent will immediately attempt to connect to Vault at `https://vault.service.consul:8200` to retrieve 
a token for the requested role.
 
The token is written to `~/.vault-token` and the nonce to `~/.vault-nonce`.

If running in agent mode, it will then block for half of the lease duration before attempting to reauthenticate with Vault 
using the nonce value stored in `~/.vault-nonce`.


## Quick start

Options for getting started:

* [Download the latest release](../../releases).
* Clone the repo: `git clone https://github.com/Brightspace/vault-ec2auth-agent.git`.

 
## Documentation

* Typical run-once usage: `vault-ec2auth-agent -role my_role` 
* Run as agent usage: `vault-ec2auth-agent -agent -role my_role`
* Additional options can be seen by running with no parameters.

### Running as an agent

By providing the `-agent` argument the agent will block until cancelled with `ctrl+c`. In this mode leases will be automatically
renewed at the half-life of the lease.

## Versioning

Vault EC2Auth Agent releases are maintained under [the Semantic Versioning guidelines](http://semver.org/).

## Contributing

Please read through our [contributing guidelines](CONTRIBUTING.md). Included are directions for opening issues, coding standards, and notes on development.
