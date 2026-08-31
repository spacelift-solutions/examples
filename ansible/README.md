# Ansible on EC2

This example configures an Apache HTTP Server on EC2 instances with Ansible. It
finds the hosts with the AWS EC2 inventory plugin, so you do not maintain a
static inventory file.

## Overview

The playbook installs Apache, writes a virtual host configuration, and serves a
page that names the Spacelift run that created it. The inventory plugin queries
EC2 for running instances that carry the `Ansible` tag, so the host list follows
your infrastructure.

## Structure

- `playbook.yml` - Installs Apache, renders the templates, and starts the service
- `aws_ec2.yml` - AWS EC2 inventory plugin configuration
- `ansible.cfg` - Ansible configuration, points the inventory at `aws_ec2.yml`
- `vars/default.yml` - Document root and port number
- `templates/spacelift.conf.j2` - Apache virtual host
- `templates/index.html.j2` - The page Apache serves

## Prerequisites

- At least one running EC2 instance with an `Ansible` tag
- An AWS integration on the stack that can call `ec2:DescribeInstances`
- Network access from the worker to the instances on port 22
- An SSH private key that reaches the instances as the `ec2-user` account

## Stack settings

Create an Ansible stack against this repository and set the following:

| Setting | Value |
| --- | --- |
| Project root | `ansible` |
| Playbook | `playbook.yml` |
| Runner image | `public.ecr.aws/spacelift/runner-ansible:latest-aws` |

First, point Ansible at the configuration file:

```shell
ANSIBLE_CONFIG=/mnt/workspace/source/ansible/ansible.cfg
```

Ansible skips an `ansible.cfg` that sits in a world-writable working directory,
and only prints a warning when it does. `ANSIBLE_CONFIG` names the file
explicitly, so the run never depends on that check. Spacelift clones the
repository into `/mnt/workspace/source/`, so the path includes the `ansible`
directory even though the project root already points there.

Then add the SSH private key as a secret
[mounted file](https://docs.spacelift.io/concepts/configuration/environment#mounted-files)
and point Ansible at it:

```shell
ANSIBLE_PRIVATE_KEY_FILE=/mnt/workspace/<name of the mounted file>
```

Without the key the playbook cannot reach the hosts. It sets
`ignore_unreachable: true`, so the run still succeeds and configures nothing.

## How it Works

1. **Inventory**: `aws_ec2.yml` asks the AWS EC2 inventory plugin for running
   instances in `us-east-1` that have a tag named `Ansible`. Change the region
   and the filters to match your account.
2. **Configuration**: `ansible.cfg` sets `aws_ec2.yml` as the inventory and
   connects as the `ec2-user` account.
3. **Run**: the playbook installs the `httpd` package, creates the document
   root from `vars/default.yml`, renders both templates, and starts the
   service. A handler restarts Apache when the virtual host changes.

## Customizing

Change the region and the tag filter in `aws_ec2.yml` to select different
hosts. Change `doc_root` and `port_number` in `vars/default.yml` to move the
site. Both templates read `port_number`, so the virtual host and the page stay
consistent.
