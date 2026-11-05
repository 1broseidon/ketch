---
url: https://click.palletsprojects.com/en/stable/testing/
title: Testing Click Applications
words: 1008
---
# Testing Click Applications

Click provides the [click.testing](https://click.palletsprojects.com/en/stable/api/#testing) module to help you invoke command line applications and check their behavior.

Warning

These tools should only be used for testing since they change the entire interpreter state for simplicity. They are not thread-safe: see [Running tests in parallel](https://click.palletsprojects.com/en/stable/testing/#running-tests-in-parallel) for the supported way to run CLI tests concurrently.

The examples use [pytest](https://docs.pytest.org/en/stable/) style tests.

## Basic Example

The key pieces are:

- [`CliRunner`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner "click.testing.CliRunner") - used to invoke commands as command line scripts.
- [`Result`](https://click.palletsprojects.com/en/stable/api/#click.testing.Result "click.testing.Result") - returned from [`CliRunner.invoke()`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner.invoke "click.testing.CliRunner.invoke"). Captures output data, exit code, optional exception, and captures the output as bytes and binary data.

hello.py[¶](https://click.palletsprojects.com/en/stable/testing/#id1 "Link to this code")

```
import click

@click.command()
@click.argument('name')
def hello(name):
   click.echo(f'Hello {name}!')
```
test\_hello.py[¶](https://click.palletsprojects.com/en/stable/testing/#id2 "Link to this code")

```
from click.testing import CliRunner
from hello import hello

def test_hello_world():
  runner = CliRunner()
  result = runner.invoke(hello, ['Peter'])
  assert result.exit_code == 0
  assert result.output == 'Hello Peter!\n'
```
## Subcommands

A subcommand name must be specified in the `args` parameter [`CliRunner.invoke()`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner.invoke "click.testing.CliRunner.invoke"):

sync.py[¶](https://click.palletsprojects.com/en/stable/testing/#id3 "Link to this code")

```
import click

@click.group()
@click.option('--debug/--no-debug', default=False)
def cli(debug):
   click.echo(f"Debug mode is {'on' if debug else 'off'}")

@cli.command()
def sync():
   click.echo('Syncing')
```
test\_sync.py[¶](https://click.palletsprojects.com/en/stable/testing/#id4 "Link to this code")

```
from click.testing import CliRunner
from sync import cli

def test_sync():
  runner = CliRunner()
  result = runner.invoke(cli, ['--debug', 'sync'])
  assert result.exit_code == 0
  assert 'Debug mode is on' in result.output
  assert 'Syncing' in result.output
```
## Context Settings

Additional keyword arguments passed to [`CliRunner.invoke()`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner.invoke "click.testing.CliRunner.invoke") will be used to construct the initial [`Context object`](https://click.palletsprojects.com/en/stable/api/#click.Context "click.Context"). For example, setting a fixed terminal width equal to 60:

sync.py[¶](https://click.palletsprojects.com/en/stable/testing/#id5 "Link to this code")

```
import click

@click.group()
def cli():
   pass

@cli.command()
def sync():
   click.echo('Syncing')
```
test\_sync.py[¶](https://click.palletsprojects.com/en/stable/testing/#id6 "Link to this code")

```
from click.testing import CliRunner
from sync import cli

def test_sync():
  runner = CliRunner()
  result = runner.invoke(cli, ['sync'], terminal_width=60)
  assert result.exit_code == 0
  assert 'Debug mode is on' in result.output
  assert 'Syncing' in result.output
```
## File System Isolation

To test a command that reads or writes files, give each test its own directory and pass absolute paths to the command. Pytest’s [`tmp_path`](https://docs.pytest.org/en/stable/how-to/tmp_path.html) fixture provides a fresh directory per test; [`tempfile.TemporaryDirectory`](https://docs.python.org/3/library/tempfile.html#tempfile.TemporaryDirectory "(in Python v3.14)") works without pytest.

cat.py[¶](https://click.palletsprojects.com/en/stable/testing/#id7 "Link to this code")

```
import click

@click.command()
@click.argument('f', type=click.File())
def cat(f):
   click.echo(f.read())
```
test\_cat.py[¶](https://click.palletsprojects.com/en/stable/testing/#id8 "Link to this code")

```
from click.testing import CliRunner
from cat import cat

def test_cat(tmp_path):
   hello = tmp_path / 'hello.txt'
   hello.write_text('Hello World!')

   runner = CliRunner()
   result = runner.invoke(cat, [str(hello)])
   assert result.exit_code == 0
   assert result.output == 'Hello World!\n'
```
Because the directory is unique to the test and the command receives an absolute path, nothing leaks between tests and the working directory is never changed.

### Running tests in parallel

Isolating the file system with pytest is thread-safe: each test, or each thread, works in its own directory through absolute paths, with no shared state.

[`CliRunner.invoke()`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner.invoke "click.testing.CliRunner.invoke") is different. While a command runs it swaps several pieces of process-global state into place (like `sys.stdout`, `sys.stderr`, `sys.stdin`, etc). Two invocations active at the same time in one interpreter overwrite each other’s state, so the runner cannot be driven from several threads at once.

Warning

Do not parallelize CLI tests with threads. Use process-based isolation instead, where each worker has its own interpreter and its own copy of those globals. [pytest-xdist](https://pytest-xdist.readthedocs.io/) does this with `pytest --numprocesses=auto`.

Click’s own test suite uses exactly this setup: the `tests-random` dependency group and the `random` tox environment in [`pyproject.toml`](https://github.com/pallets/click/blob/main/pyproject.toml) run the suite under `pytest-randomly` and `pytest-xdist` to surface ordering bugs and races.

Deprecated since version 8.5.0: [`CliRunner.isolated_filesystem()`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner.isolated_filesystem "click.testing.CliRunner.isolated_filesystem") is deprecated and will be removed in Click 9.0. Use a temporary directory with absolute paths, as shown above.

## Input Streams

The test wrapper can provide input data for the input stream (stdin). This is very useful for testing prompts.

prompt.py[¶](https://click.palletsprojects.com/en/stable/testing/#id9 "Link to this code")

```
import click

@click.command()
@click.option('--foo', prompt=True)
def prompt(foo):
   click.echo(f"foo={foo}")
```
test\_prompt.py[¶](https://click.palletsprojects.com/en/stable/testing/#id10 "Link to this code")

```
from click.testing import CliRunner
from prompt import prompt

def test_prompts():
   runner = CliRunner()
   result = runner.invoke(prompt, input='wau wau\n')
   assert not result.exception
   assert result.output == 'Foo: wau wau\nfoo=wau wau\n'
```
Prompts will be emulated so they write the input data to the output stream as well. If hidden input is expected then this does not happen.

## Capture modes

[`CliRunner`](https://click.palletsprojects.com/en/stable/api/#click.testing.CliRunner "click.testing.CliRunner") captures output by replacing `sys.stdout` and `sys.stderr` with in-memory wrappers. The `capture` parameter controls which strategy is used.

### `capture="sys"` (default)

Captures Python-level writes (`print()`, `click.echo()`, `sys.stdout.write()`). It is fast and sufficient for most Click applications.

Code that holds a reference to the original `sys.stdout` (like a library that does `from sys import stdout` at import time) bypasses the capture and its output is lost.

In this mode `sys.stdout.fileno()` and `sys.stderr.fileno()` raise [`io.UnsupportedOperation`](https://docs.python.org/3/library/io.html#io.UnsupportedOperation "(in Python v3.14)"), matching the pre-`8.3.3` behavior. C-level consumers ([`faulthandler`](https://docs.python.org/3/library/faulthandler.html#module-faulthandler "(in Python v3.14)"), [`subprocess`](https://docs.python.org/3/library/subprocess.html#module-subprocess "(in Python v3.14)"), C extensions) that expect a real file descriptor must opt into the `capture="fd"` mode.

### `capture="fd"`

Redirects OS file descriptors `1` and `2` to a temporary file via [`os.dup2()`](https://docs.python.org/3/library/os.html#os.dup2 "(in Python v3.14)"), inspired by [Pytest’s `capfd`](https://docs.pytest.org/en/stable/how-to/capture-stdout-stderr.html). This catches output that bypasses `sys.stdout`, including:

- Stale references to the original `sys.stdout` and `sys.stderr`.
- Logging frameworks that cache the original stream (like `structlog` or the stdlib’s `logging` module).
- C extensions and subprocesses that write directly to `fd 1` or `fd 2`.

```
from click.testing import CliRunner
from myapp import cli


def test_captures_everything():
    runner = CliRunner(capture="fd")
    result = runner.invoke(cli)
    # result.stdout contains both Python-level and fd-level output
    assert "expected output" in result.stdout
```
In this mode `sys.stdout.fileno()` returns the saved (pre-redirection) `fd`, so [`faulthandler`](https://docs.python.org/3/library/faulthandler.html#module-faulthandler "(in Python v3.14)") and similar consumers keep working. Writes to `fd 1` and `fd 2` land in the capture tmpfile, so `os.dup2()` calls inside the CLI no longer leak into the host runner’s stdout.

Note

`capture="fd"` is not available on Windows.

Changelog

Changed in version 8.4.0: Added the `capture` parameter. The default `sys` mode no longer exposes the original `fd` through `fileno()`, reverting the change introduced in `8.3.3` that broke Pytest’s `fd`-level capture teardown. Use `capture="fd"` to restore that behavior with proper isolation.
