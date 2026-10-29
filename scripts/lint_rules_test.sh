#!/bin/sh
# lint_rules_test.sh: prove the import and identifier rules in .golangci.yml (Constitution IV)
# deny what they must and nothing more. It builds a throwaway module in a mktemp dir with this
# repo's go.mod and .golangci.yml, one stub package per layer, one probe package per case, and a
# third-party module (example.com/evil, wired in with a replace) whose package calls http.Get.
# Each case file imports the listed packages (blank imports, or plain ones when the case has
# code). scripts/lint.sh (golangci-lint once per RELEASE_TARGETS target with cgo off, then once
# on the host with cgo on; depguard and forbidigo only, every issue kept, not one per line) then lints them all.
# Each case's issues are found by an anchored match on its file's module-relative path, and the
# set of rules they name (depguard list names, or "forbidigo"), unioned over every pass, must
# equal the case's want exactly, or be empty for "clean". The temp dir is removed on every
# exit. Output names only module-relative paths. Exit: 0 every case as expected, 1 a case
# mismatched, 2 setup, build, lint or grep error. LINT_RULES_CONFIG names another config to test
# (RED records and mutants); it defaults to the repo's .golangci.yml.
set -u
root=$(git rev-parse --show-toplevel) || exit 2
d=$(mktemp -d) || exit 2
trap 'rm -rf "$d"' EXIT
trap 'exit 2' INT TERM
M=github.com/Exploded-views/yo-human

# dir|want (clean, or the sorted, space-separated rules that must deny it)|file|imports
# (M/ = this module; alias=path imports path under alias, dot=path is a dot import)|what it
# shows|code (optional; when set, the imports are plain, not blank)|build constraint (optional)|
# pass (optional: text that the header of at least one lint.sh pass reporting the case must
# contain, such as GOOS=darwin, so a case only some pass can see proves that pass ran)
cases='
cmd/yo-human/casecore|cmd-only-cli|x.go|M/internal/core|cmd imports core
cmd/yo-human/caseok|clean|x.go|M/internal/cli M/internal/version os|cmd imports cli, version and the standard library
internal/store/casehttp|internal-no-network|x.go|net/http|store imports net/http
internal/store/casenet|internal-no-network|x.go|net|store imports net
internal/store/casetls|internal-no-network|x.go|crypto/tls|store imports crypto/tls
internal/config/casehttp|internal-no-network|x.go|net/http|config imports net/http
internal/core/casehttp|internal-no-network|x.go|net/http|core imports net/http
internal/core/casehttputil|internal-no-network|x.go|net/http/httputil|core imports net/http/httputil
internal/cli/casehttp|internal-no-network|x.go|net/http|a cli file not named _e2e imports net/http
internal/store/caseurl|clean|x.go|net/url net/mail net/netip|net/url, net/mail and net/netip stay allowed
internal/core/casenocgo|internal-no-network|x.go|net/http|core imports net/http in a !cgo file (the release has cgo off)||!cgo|CGO_ENABLED=0
internal/core/casedarwin|internal-no-network|x_darwin.go|net/http|core imports net/http in an _darwin.go file|||GOOS=darwin
internal/core/caselinux|internal-no-network|x_linux.go|net/http|core imports net/http in an _linux.go file|||GOOS=linux
internal/core/casearm64|internal-no-network|x_arm64.go|net/http|core imports net/http in an _arm64.go file|||GOARCH=arm64 CGO_ENABLED=0
internal/core/casecgotag|internal-no-network|x.go|net/http|core imports net/http in a cgo-only file (test builds use cgo)||cgo|CGO_ENABLED=1
internal/store/casetest|clean|x_test.go|net/http|net/http in a store test file
internal/core/casenetrpc|internal-no-network|x.go|net/rpc|core imports net/rpc
internal/core/casesmtp|internal-no-network|x.go|net/smtp|core imports net/smtp
internal/core/casetextproto|internal-no-network|x.go|net/textproto|core imports net/textproto
internal/core/casesyslog|internal-no-network|x.go|log/syslog|core imports log/syslog
internal/store/casenetrpc|internal-no-network|x.go|net/rpc|store imports net/rpc
internal/store/casesmtp|internal-no-network|x.go|net/smtp|store imports net/smtp
internal/store/casetextproto|internal-no-network|x.go|net/textproto|store imports net/textproto
internal/store/casesyslog|internal-no-network|x.go|log/syslog|store imports log/syslog
internal/channel/ntfy/casehttp|clean|x.go|net/http net/smtp log/syslog|a channel implementation imports net/http, net/smtp and log/syslog
internal/secrets/caseexec|internal-no-exec|x.go|os/exec|secrets imports os/exec
internal/core/caseexec|internal-no-exec|x.go|os/exec|core imports os/exec
internal/channel/ntfy/caseexec|internal-no-exec|x.go|os/exec|a channel implementation imports os/exec
internal/core/caseexectest|clean|x_test.go|os/exec|os/exec in a core test file
tools/perf/caseexec|clean|x.go|os/exec|tools/perf imports os/exec
internal/core/casesocket|forbidigo|x.go|syscall|core calls syscall.Socket|var _ = syscall.Socket
internal/core/caseconnect|forbidigo|x.go|syscall|core calls syscall.Connect|var _ = syscall.Connect
internal/store/casesocket|forbidigo|x.go|syscall|store calls syscall.Socket|var _ = syscall.Socket
internal/store/caseconnect|forbidigo|x.go|syscall|store calls syscall.Connect|var _ = syscall.Connect
internal/store/casesendto|forbidigo|x.go|syscall|store calls syscall.Sendto|var _ = syscall.Sendto
internal/store/casesendmsg|forbidigo|x.go|syscall|store calls syscall.Sendmsg|var _ = syscall.Sendmsg
internal/store/casebind|forbidigo|x.go|syscall|store calls syscall.Bind|var _ = syscall.Bind
internal/channel/registry/casesocket|forbidigo|x.go|syscall|registry calls syscall.Socket|var _ = syscall.Socket
internal/core/casesockalias|forbidigo|x.go|sc=syscall|core calls syscall.Socket through an alias|var _ = sc.Socket
internal/store/casesockdot|forbidigo|x.go|dot=syscall|store calls Socket through a dot import of syscall|var _ = Socket
internal/core/casesyscall|forbidigo|x.go|syscall|core calls syscall.Syscall (SYS_SOCKET)|var _ = syscall.Syscall
internal/core/casesyscall6|forbidigo|x.go|syscall|core calls syscall.Syscall6|var _ = syscall.Syscall6
internal/store/caserawsyscall|forbidigo|x.go|syscall|store calls syscall.RawSyscall (SYS_CONNECT)|var _ = syscall.RawSyscall
internal/store/caserawsyscall6|forbidigo|x.go|syscall|store calls syscall.RawSyscall6|var _ = syscall.RawSyscall6
internal/core/caseplugin|internal-no-plugin|x.go|plugin|core imports plugin
internal/channel/ntfy/caseplugin|internal-no-plugin|x.go|plugin|a channel implementation imports plugin
internal/core/caseplugintest|internal-no-plugin|x_test.go|plugin|a core test imports plugin
internal/store/caseflock|clean|x.go|syscall|store calls syscall.Flock (syscall stays allowed)|var _ = syscall.Flock
internal/channel/ntfy/casesocket|clean|x.go|syscall|a channel implementation calls syscall.Socket|var _ = syscall.Socket
internal/core/caseevil|core-imports|x.go|example.com/evil|core imports an unreviewed third-party module
internal/store/caseevil|store-imports|x.go|example.com/evil|store imports an unreviewed module whose package calls http.Get
internal/config/caseevil|config-imports|x.go|example.com/evil|config imports an unreviewed third-party module
internal/secrets/caseevil|secrets-imports|x.go|example.com/evil|secrets imports an unreviewed third-party module
internal/problem/caseevil|problem-imports|x.go|example.com/evil|problem imports an unreviewed third-party module
internal/cli/caseevil|cli-imports|x.go|example.com/evil|cli imports an unreviewed third-party module
internal/store/caseeviltest|clean|x_test.go|example.com/evil|a store test imports a third-party module
internal/config/casentfy|channel-impls-only-via-registry|x.go|M/internal/channel/ntfy|config imports a channel implementation
internal/secrets/casentfy|channel-impls-only-via-registry|x.go|M/internal/channel/ntfy|secrets imports a channel implementation
internal/config/caseiface|clean|x.go|M/internal/channel|config imports the channel interface
internal/core/casentfytest|channel-impls-only-via-registry-tests|x_test.go|M/internal/channel/ntfy|a core test imports a channel implementation
internal/core/casechanneltest|clean|x_test.go|M/internal/channel/channeltest|a core test imports channeltest
internal/cli/casestore|cli-not-store-or-impls|x.go|M/internal/store|cli imports store
internal/cli/casentfy|channel-impls-only-via-registry cli-not-store-or-impls|x.go|M/internal/channel/ntfy|cli imports a channel implementation
internal/cli/caseok|clean|x.go|M/internal/core M/internal/config M/internal/secrets M/internal/channel M/internal/channel/registry M/internal/problem M/internal/version|cli imports everything but store and implementations
internal/cli/casetest|clean|x_test.go|M/internal/store|a cli test imports store
internal/core/casentfy|channel-impls-only-via-registry core-no-network|x.go|M/internal/channel/ntfy|core imports a channel implementation
internal/core/casetransport|channel-impls-only-via-registry core-no-network|x.go|M/internal/channel/transport|core imports internal/channel/transport
internal/channel/ntfy/casetransport|clean|x.go|M/internal/channel/transport M/internal/channel|a channel implementation imports transport and the interface
internal/channel/ntfy/casebark|channel-implementations|x.go|M/internal/channel/bark|a channel implementation imports another (bark)
internal/channel/ntfy/casecore|channel-implementations|x.go|M/internal/core|a channel implementation imports core
internal/channel/ntfy/casesecrets|channel-implementations|x.go|M/internal/secrets|a channel implementation imports secrets
internal/channel/ntfy/casecoretest|channel-implementation-tests|x_test.go|M/internal/core|a channel implementation test imports core
internal/store/casecore|store-is-the-bottom|x.go|M/internal/core|store imports core
internal/channel|channel-interface|casestore.go|M/internal/store|the internal/channel interface imports store
internal/config/casecore|below-core|x.go|M/internal/core|config imports core
internal/problem/casestore|problem-is-a-leaf|x.go|M/internal/store|problem imports store
tools/perf/casecli|cli-is-the-top|x.go|M/internal/cli|tools/perf imports cli
internal/channel/registry/casentfy|clean|x.go|M/internal/channel/ntfy M/internal/channel M/internal/problem|registry imports an implementation, the interface and problem
internal/channel/registry/casehttp|registry-no-network|x.go|net/http|registry imports net/http
internal/channel/registry/casehttptest|registry-no-network|x.go|net/http/httptest|registry imports net/http/httptest
internal/channel/registry/casenetrpc|registry-no-network|x.go|net/rpc|registry imports net/rpc
internal/channel/registry/casestore|registry-imports|x.go|M/internal/store|registry imports store
internal/channel/registry/casecore|registry-imports|x.go|M/internal/core|registry imports core
internal/channel/registry/caseconfig|registry-imports|x.go|M/internal/config|registry imports config
internal/hosted/bus/casesql|hosted-bus-imports|x.go|database/sql|hosted bus imports database/sql
internal/hosted/bus/casehttp|internal-no-network|x.go|net/http|hosted bus imports net/http
internal/hosted/bus/casestore|hosted-imports|x.go|M/internal/store|hosted bus imports the CLI store
internal/hosted/bus/casenotify|hosted-bus-imports|x.go|M/internal/hosted/notify|hosted bus imports notify (bus is standard library only)
internal/hosted/bus/casepolicy|clean|x.go|M/internal/hosted/policy|hosted bus imports policy (the approval policy)
internal/hosted/policy/casebus|hosted-policy-leaf|x.go|M/internal/hosted/bus|hosted policy imports bus (policy is a leaf)
internal/hosted/policy/caseversion|hosted-policy-leaf|x.go|M/internal/version|hosted policy imports version (standard library only)
internal/hosted/policy/casehttp|internal-no-network|x.go|net/http|hosted policy imports net/http
internal/hosted/policy/caseok|clean|x.go|encoding/json path strings|hosted policy imports the standard library
internal/hosted/db/casehttp|internal-no-network|x.go|net/http|hosted db imports net/http
internal/hosted/db/caseok|clean|x.go|M/internal/hosted/bus database/sql|hosted db imports bus and database/sql
internal/hosted/db/casesqlite|clean|x.go|modernc.org/sqlite|hosted db imports the reviewed SQLite driver
internal/hosted/db/casesqlitelib|hosted-db-imports|x.go|modernc.org/sqlite/lib|hosted db imports a package of the driver that is not named
internal/hosted/db/caseevil|hosted-db-imports|x.go|example.com/evil|hosted db imports an unreviewed module
internal/hosted/db/casepolicy|hosted-db-imports|x.go|M/internal/hosted/policy|hosted db imports policy (bus carries it)
internal/hosted/db/casenotify|hosted-db-imports|x.go|M/internal/hosted/notify|hosted db imports notify
internal/hosted/db/caseweb|hosted-db-imports|x.go|M/internal/hosted/web|hosted db imports web
internal/hosted/db/casestore|hosted-db-imports|x.go|M/internal/store|hosted db imports the CLI store
internal/hosted/bus/casesqlite|hosted-imports|x.go|modernc.org/sqlite|hosted bus imports the SQLite driver
internal/hosted/web/casesqlite|hosted-imports|x.go|modernc.org/sqlite|hosted web imports the SQLite driver
internal/core/casesqlite|core-imports|x.go|modernc.org/sqlite|core imports the SQLite driver
internal/hosted/notify/casehttp|internal-no-network|x.go|net/http|hosted notify imports net/http
internal/hosted/egress/bark/casebus|hosted-egress-imports|x.go|M/internal/hosted/bus|an egress package imports bus
internal/hosted/egress/bark/caseok|clean|x.go|net/http M/internal/hosted/notify|an egress package imports net/http and notify
internal/hosted/egress/bark/caseclient|clean|x.go|net/http|an egress package builds an http.Client|var _ = &http.Client{}
internal/hosted/web/casehttp|clean|x.go|net/http|hosted web imports net/http (inbound)
internal/hosted/oauth/casehttp|clean|x.go|net/http|hosted oauth imports net/http (inbound)
internal/hosted/web/caseegress|hosted-egress-only-via-app|x.go|M/internal/hosted/egress/bark|hosted web imports an egress package
internal/hosted/app/caseegress|clean|x.go|M/internal/hosted/egress/bark M/internal/hosted/web M/internal/version|hosted app imports egress, web and version
internal/hosted/web/caseclient|forbidigo|x.go|net/http|hosted web builds an http.Client (outbound only in egress)|var _ = &http.Client{}
internal/hosted/oauth/caseget|forbidigo|x.go|net/http|hosted oauth calls http.Get (outbound only in egress)|var _ = http.Get
internal/hosted/web/casedefault|forbidigo|x.go|net/http|hosted web uses http.DefaultClient|var _ = http.DefaultClient
internal/hosted/web/caseclienttest|clean|x_test.go|net/http|a hosted web test calls http.Get|var _ = http.Get
internal/hosted/web/casedial|hosted-inbound-no-dial|x.go|net|hosted web imports net (net.Dial)
internal/hosted/oauth/casetls|hosted-inbound-no-dial|x.go|crypto/tls|hosted oauth imports crypto/tls (tls.Dial)
internal/hosted/web/caseproxy|hosted-inbound-no-dial|x.go|net/http/httputil|hosted web imports httputil (reverse proxy)
internal/hosted/oauth/casesmtp|hosted-inbound-no-dial|x.go|net/smtp|hosted oauth imports net/smtp
internal/hosted/web/caserpc|hosted-inbound-no-dial|x.go|net/rpc|hosted web imports net/rpc
internal/hosted/web/caseserveok|clean|x.go|net/http net/url net/netip|hosted web imports net/http, net/url and net/netip (serving)
internal/hosted/web/casetransport|forbidigo|x.go|net/http|hosted web uses http.DefaultTransport|var _ = http.DefaultTransport
internal/hosted/oauth/casetransportlit|forbidigo|x.go|net/http|hosted oauth builds an http.Transport|var _ = &http.Transport{}
internal/hosted/egress/bark/casetransport|clean|x.go|net/http|an egress package builds an http.Transport|var _ = &http.Transport{}
internal/hosted/app/caseevil|hosted-imports|x.go|example.com/evil|a hosted package imports an unreviewed module
internal/core/casehosted|cli-side-not-hosted|x.go|M/internal/hosted/bus|core imports the hosted bus
cmd/yo-human/casehosted|cli-side-not-hosted cmd-only-cli|x.go|M/internal/hosted/app|the CLI main imports the hosted app
cmd/yo-human-hosted/casebus|cmd-hosted-only-app|x.go|M/internal/hosted/bus|the hosted main imports bus
cmd/yo-human-hosted/caseok|clean|x.go|M/internal/hosted/app M/internal/version os|the hosted main imports app, version and the standard library
'

cfg=${LINT_RULES_CONFIG:-$root/.golangci.yml}
cp "$root/go.mod" "$root/go.sum" "$d/" && cp "$cfg" "$d/.golangci.yml" || exit 2
printf '\nrequire example.com/evil v0.0.0\n\nreplace example.com/evil => ./thirdparty/evil\n' >> "$d/go.mod" || exit 2
mkdir -p "$d/thirdparty/evil" || exit 2
printf 'module example.com/evil\n\ngo 1.27\n' > "$d/thirdparty/evil/go.mod" || exit 2
cat > "$d/thirdparty/evil/evil.go" <<'GO' || exit 2
// Package evil stands for an unreviewed module that opens connections of its own.
package evil

import "net/http"

func init() {
	if resp, err := http.Get("http://example.com/"); err == nil {
		_ = resp.Body.Close()
	}
}
GO
for s in internal/cli internal/version internal/core internal/store internal/config internal/secrets \
  internal/problem internal/channel internal/channel/ntfy internal/channel/bark \
  internal/channel/channeltest internal/channel/registry internal/channel/transport \
  internal/hosted/app internal/hosted/bus internal/hosted/db internal/hosted/web internal/hosted/oauth \
  internal/hosted/notify internal/hosted/egress/bark internal/hosted/policy; do
  mkdir -p "$d/$s" && echo "package $(basename "$s")" > "$d/$s/stub.go" || exit 2
done
echo "$cases" | while IFS='|' read -r dir want file imps what code tag pass; do
  [ -z "$dir" ] && continue
  pkg=$(basename "$dir")
  mkdir -p "$d/$dir" || exit 2
  blank='_ '
  [ -n "$code" ] && blank=''
  {
    if [ -n "$tag" ]; then printf '//go:build %s\n\n' "$tag"; fi
    echo "package $pkg"
    echo
    echo "import ("
    for i in $imps; do
      name=$blank
      case "$i" in
        dot=*) name='. ' i=${i#dot=} ;;
        *=*) name="${i%%=*} " i=${i#*=} ;;
      esac
      echo "	$name\"$(echo "$i" | sed "s#^M/#$M/#")\""
    done
    echo ")"
    if [ -n "$code" ]; then printf '\n%s\n' "$code"; fi
  } > "$d/$dir/$file" || exit 2
  # Every case package has an untagged file, so it builds under every target and cgo setting.
  if [ "$file" != x.go ] || [ -n "$tag" ]; then echo "package $pkg" > "$d/$dir/doc.go" || exit 2; fi
done || exit 2

cd "$d" || exit 2
# The run below uses --enable-only, which turns both linters on whatever the config says, so the
# config itself must enable them: otherwise the real lint would skip a rule this test passes.
fail=0
if ! golangci-lint linters -c "$d/.golangci.yml" > "$d/linters.log" 2>&1; then
  echo "lint_rules_test: golangci-lint linters failed:"; sed "s#$d/##g" "$d/linters.log"; exit 2
fi
sed -n '/^Enabled by your configuration linters:/,/^Disabled by your configuration linters:/p' \
  "$d/linters.log" > "$d/enabled.log"
for l in depguard forbidigo; do
  if grep -q -e "^$l:" -e "^$l " "$d/enabled.log"; then
    echo "ok   config: $l is enabled in .golangci.yml"
  else
    echo "FAIL config: $l is not enabled in .golangci.yml (linters.enable)"; fail=1
  fi
done
if ! go vet ./... > "$d/vet.log" 2>&1; then
  echo "lint_rules_test: the probe module does not build:"; sed "s#$d/##g" "$d/vet.log"; exit 2
fi
# scripts/lint.sh, as make lint runs it: once per RELEASE_TARGETS target with cgo off, then once
# on the host with cgo on. A case's issues are the union over every pass.
sh "$root/scripts/lint.sh" --enable-only depguard,forbidigo --uniq-by-line=false \
  --max-issues-per-linter 0 --max-same-issues 0 --output.text.print-issued-lines=false \
  --show-stats=false ./... > "$d/lint.log" 2>&1
status=$?
if [ "$status" -ne 0 ] && [ "$status" -ne 1 ]; then
  echo "lint_rules_test: scripts/lint.sh exit $status:"; sed "s#$d/##g" "$d/lint.log"; exit 2
fi

# rules FILE: the sorted, space-separated rule names in FILE's issues ("unparsed" for an issue
# that names none).
rules() {
  while IFS= read -r line; do
    case "$line" in
      *"not allowed from list '"*) echo "$line" | sed "s/.*not allowed from list '\([^']*\)'.*/\1/" ;;
      *"(forbidigo)") echo forbidigo ;;
      *) echo unparsed ;;
    esac
  done < "$1" | sort -u | tr '\n' ' ' | sed 's/ $//'
}

total=0
while IFS='|' read -r dir want file imps what code tag pass; do
  [ -z "$dir" ] && continue
  total=$((total + 1))
  # Anchored at the line start, dots escaped: one case's issues never match another's file.
  pat=$(printf '%s' "$dir/$file:" | sed 's/[.]/\\./g')
  grep -e "^$pat" "$d/lint.log" > "$d/case.log"
  rc=$?
  if [ "$rc" -gt 1 ]; then echo "lint_rules_test: grep failed (exit $rc)"; exit 2; fi
  got=clean
  if [ "$rc" -eq 0 ]; then got=$(rules "$d/case.log"); fi
  if [ "$got" = "$want" ] && [ -n "$pass" ] && ! awk -v pat="$dir/$file:" -v need="$pass" '
      /^== golangci-lint / { inpass = index($0, need) > 0; next }
      inpass && index($0, pat) == 1 { found = 1 }
      END { exit !found }' "$d/lint.log"; then
    got="$got, but no pass with $pass reported it"
  fi
  if [ "$got" = "$want" ]; then
    echo "ok   $dir/$file: $what ($want${pass:+, seen by a $pass pass})"
  else
    echo "FAIL $dir/$file: $what: want $want, got $got"
    sed "s#$d/##g" "$d/case.log"
    fail=1
  fi
done <<EOF
$cases
EOF
if [ "$fail" -ne 0 ]; then echo "lint_rules_test: a case failed"; exit 1; fi
echo "lint_rules_test: all $total cases pass"
