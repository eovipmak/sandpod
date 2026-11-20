# Shared PHP runtimes

PHP is installed once on the host as parallel Remi SCL runtimes. Customer
containers mount those runtime trees read-only, but each customer runs its own
PHP-FPM master and workers inside its rootless container.

## Runtime layout

- PHP 8.2: `/opt/remi/php82/root/usr`, config `/etc/opt/remi/php82`
- PHP 8.3: `/opt/remi/php83/root/usr`, config `/etc/opt/remi/php83`
- PHP 8.4: `/opt/remi/php84/root/usr`, config `/etc/opt/remi/php84`
- PHP 8.5: `/opt/remi/php85/root/usr`, config `/etc/opt/remi/php85`
- PHP 8.6: `/opt/remi/php86/root/usr`, config `/etc/opt/remi/php86`

The RPM package manager keeps the newest patch installed for each minor
namespace. The customer-facing selection is 8.2, 8.3, 8.4, 8.5, or 8.6;
patch
display can be added to the control panel later. PHP 8.0/8.1 are end-of-life
and intentionally omitted.

## Install

Run `scripts/install-php-runtimes.sh` as root. It installs the parallel
PHP-FPM/CLI packages and common extensions, but does not enable or start
PHP-FPM services on the host.

When SELinux is enforcing, the installer labels the shared runtime/config paths
for container reads. Customer containers mount the paths read-only.

## Apply a patch

Update the host RPMs to the newest patch:

```sh
sudo modules/shared-runtimes/scripts/update-php-runtimes.sh
```

Package files update once on the host. Existing PHP-FPM processes continue
using code already loaded in memory, so reload only the affected PHP-FPM minor
inside each customer container:

```sh
sudo modules/shared-runtimes/scripts/reload-customer-php.sh \
  svc-c001 customer-001 8.3
```

The helper validates the FPM configuration, sends `USR2` to that FPM master,
and confirms it remains running. This gracefully reloads that PHP version
without recreating the container or restarting Nginx, SSH, cron, or other
PHP-FPM minors. Roll out to customers in batches and health-check each one.

The provisioning/update controller should call the reload helper once per
customer using the customer-to-service-account/container mapping.

## Scope

PHP 8.2, 8.3, 8.4, 8.5, and 8.6 use shared read-only host runtimes. Node.js 22/24/26
uses shared read-only host trees at `/opt/sandpod/node/<major>` (see
`scripts/install-node-runtimes.sh`, quadlet mount, `reload-customer-app.sh`
for `supervisorctl restart` after an update). Python 3.12/3.13/3.14 still
ships in the image; the interim is host RPMs (`install-python-runtimes.sh`)
with a TODO for self-contained `/opt/sandpod/python/<ver>` prefixes.
