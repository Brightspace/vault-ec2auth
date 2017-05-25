# Vault-Ec2Auth

## 0.2.1

### NOTES

* Dropped the alpha tag from the versioning. [Semantic versioning](http://semver.org) applies.

### IMPROVEMENTS

* Apply the retry delay when waiting for the vault server to become available

## 0.2.0-alpha

### FEATURES:

* Automatically retry if a login error occurs such as if the ec2-auth role does not yet exist. The `retry-delay` parameter controls the time between attempts.


### BUG FIXES:

* Fixed -vault-url switch has no effect (#1)


## 0.1.1-alpha

### IMPROVEMENTS:

* change homedir lookup to behave as expected in certain scenarios
