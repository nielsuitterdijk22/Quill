# Set up SSH

SSH keys let you push and pull without a password or token prompt. Set it up once and forget about it.

## Generate a key (if you don't have one)

```bash
ssh-keygen -t ed25519 -C "your@email.com"
```

Accept the default file location (`~/.ssh/id_ed25519`). Add a passphrase if you want extra security.

If you already have a key at `~/.ssh/id_ed25519.pub` (or `id_rsa.pub`), skip this step.

## Add the key to Quill

Copy your public key:

```bash
cat ~/.ssh/id_ed25519.pub
```

In Quill, go to **Settings → SSH keys → Add SSH key**. Paste the key, give it a name (e.g. "MacBook Pro"), and save.

## Test the connection

```bash
ssh -T git@quill.so
```

You should see something like:

```text
Hi your-username! You've successfully authenticated.
```

## Use SSH for cloning

When you clone or add a remote, use the SSH URL instead of HTTPS:

```bash
git clone git@quill.so:your-username/my-project.git
```

SSH URLs on Quill follow the format `git@quill.so:owner/repo.git`.

## Multiple keys

If you use multiple SSH identities (e.g. personal and work), add this to your `~/.ssh/config`:

```text
Host quill.so
  HostName quill.so
  User git
  IdentityFile ~/.ssh/id_ed25519_quill
```

## Manage your keys

Go to **Settings → SSH keys** to see all your keys, when they were last used, and to revoke any you no longer need.
