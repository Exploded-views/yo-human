# Set up the cron flush

yo-human is not a daemon. It sends what is due each time you call it, and `yo-human flush` is the call that does only that. A cron entry that runs `flush` every few minutes is **required** for quiet-hours holds on channels that cannot schedule a later delivery.

## Inputs

Export each one in your shell before you start.

- `BIN_DIR="<directory that holds yo-human>"` — cron's PATH is short, so the entry names where the binary is
- `INSTANCE="<a channel that reaches you>"` — the channel for the final test
- `PROJECT_CONFIG="<path of a project config file>"` — only for the per-project entry below; the absolute path of a `.yo-human.yaml` or an `--config` file

## Already set

- `yo-human` is installed and at least one channel works (`yo-human channel test <instance>` printed `RESULT delivered`).
- Your levels use `quiet_hours: hold` and a `quiet_hours:` window is configured, or you want failed sends retried without waiting for the next `notify`.
- Cron runs as the same user that owns the yo-human config, secrets and state directories.

## Why it is needed

A level with `quiet_hours: hold` defers each send until its window ends:

- **ntfy** schedules the delivery itself, on the server. A held ntfy page needs no cron.
- **Bark and PagerDuty** cannot schedule. yo-human holds the send in its queue, and the first `notify` or `flush` after the window ends delivers it. If no agent calls `notify` then, the page waits for you to look, which is the opposite of what a hold is for. The cron entry makes the flush happen.

The same entry retries sends that failed transiently and are waiting for their next attempt.

## Install the entry

```sh
( crontab -l 2>/dev/null | grep -vxF -e "PATH=$BIN_DIR:/usr/bin:/bin" -e "*/5 * * * * yo-human flush"
  echo "PATH=$BIN_DIR:/usr/bin:/bin"; echo "*/5 * * * * yo-human flush" ) | crontab -
```

Running it again does not add a second copy: it drops any identical lines before it adds them.

That installs this crontab entry:

```
*/5 * * * * yo-human flush
```

Cron mails you whatever the command prints, and a flush that had nothing to do still prints `RESULT ok`. Add `>/dev/null` to the entry to stop the mail. Dead letters still show in `yo-human status`.

## Which config a cron flush uses

A cron flush reads the config of **its own directory**: the user config, plus any project file found from the directory cron runs in (cron starts in your home directory). A message queued under a different config, that is a `--config` file or a project file in some repository, is pinned to the destinations that config named. A plain cron flush will not send it somewhere else. It leaves the message parked, and `yo-human status` shows the entry with a `reason:` saying why. For a message queued under a `--config` file, the reason names that file. For one queued under a project file, it says only that the channel now points somewhere else, so run the flush from the project's directory (or a cron entry per project, below). Nothing is lost or dead-lettered.

To flush those messages, name the config, or run a cron entry per project:

```sh
yo-human flush --config "$PROJECT_CONFIG"
( crontab -l 2>/dev/null | grep -vxF -e "*/5 * * * * yo-human flush --config $PROJECT_CONFIG"
  echo "*/5 * * * * yo-human flush --config $PROJECT_CONFIG" ) | crontab -
```

A project file counts only after you trust it with `yo-human config trust`, run in your own terminal (it shows the file and asks you to type `yes`). An untrusted one is ignored, and every command says so on stderr. A `--config` file outside `$XDG_CONFIG_HOME/yo-human/` is held to the same rule: until you trust it, a command that names it exits 78, and it may never define a `log` channel.

## Exit codes

`flush` exits by what it attempted:

- **0:** everything it attempted was sent, or it attempted nothing. Parked entries are not attempts, so they never change the exit code.
- **75:** an attempted send failed again and is queued. The output has `next`, with when to try again and how many attempts are left.
- **69:** an attempted send was rejected for good and went to the dead letter. Run `yo-human status` and tell the human; retrying cannot help.

## Check it

```sh
crontab -l
yo-human status
yo-human channel test "$INSTANCE"
```

`crontab -l` should show the entry. `yo-human status` should show `queued: 0`, or each queued entry with the reason it is waiting.
