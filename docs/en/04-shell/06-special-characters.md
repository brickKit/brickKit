# Special characters

## The problem

Members' config goes into one JSON string, handed to the shell as one environment variable. A config value can hold
anything: a multi-line PEM private key, quotes, backslashes, `$`. If the platform just wrote `${VAR}` into the JSON as it
is and left it to Docker Compose to substitute at start-up, Compose would do **a plain-text substitution that knows nothing
about JSON**: substitute a value with a quote or a newline, and the JSON breaks. The shell fails to parse it at start-up —
and it only shows up at run time.

## What the platform does

**Evaluate first.** When generating the deployment files, the CLI works out the value of every member's config:

| Written as | At generation time |
| --- | --- |
| A literal | Kept as it is |
| `$var:NAME` | Replaced by the shared variable's value |
| `${VAR}` | Expanded from the process environment, then the project root's `.env`; if it isn't found, it fails loudly |
| `file://path` | Read into the file's contents |
| `{ existingSecret, key }` | Refused: the value lives only in the cluster, the CLI can't read it, and the JSON needs the value |

**Then encode it as JSON.** The evaluated values are encoded by JSON's rules: quotes, backslashes and newlines are all
escaped.

**Then escape once more for where it lands.** On Docker the JSON goes into an env file with mode 0600, with `$` written as
`$$` so that Compose substitutes nothing in it; on Kubernetes it goes into the generated Secret.

## An example

`shop/stock`'s warehouse name is kept in a file, holding quotes, a `$` and two lines:

```text
Warehouse "No. 1"
Price $5
```

```yaml
# config/shop-stock.yaml
STOCK_WAREHOUSE: file://.secrets/warehouse.txt
```

In the shell's env file:

```text
BRICKKIT_SERVED_MEMBERS_CONFIG="[{\"componentId\":\"shop/cart\",\"version\":\"0.1.0\",\"httpPort\":8081,\"extraPorts\":[],\"config\":{\"SHOP_ORDER_ENDPOINT\":\"http://shop-order-0-1-0:8083\",\"SHOP_STOCK_ENDPOINT\":\"http://shop-shell-0-1-0:8082\"}},{\"componentId\":\"shop/stock\",\"version\":\"0.1.0\",\"httpPort\":8082,\"extraPorts\":[],\"config\":{\"STOCK_WAREHOUSE\":\"Warehouse \\\"No. 1\\\"\\nPrice $$5\\n\"}}]"
```

The value the `shop/stock` module in the shell gets is the file's contents, byte for byte:

```json
{"count":42,"sku":"A1","warehouse":"Warehouse \"No. 1\"\nPrice $5\n"}
```

## Values JSON can't carry: fail loudly

JSON carries only text. When a value isn't valid UTF-8 (a binary file, say), encoding would silently replace the invalid
bytes with the replacement character — and the member would get an altered value. The platform stops it at generation
time:

```text
❌ Error: config item STOCK_WAREHOUSE of shell member shop/stock@0.1.0 is not valid UTF-8 text
   Reason: JSON can only carry text; binary bytes would be replaced silently
   Suggestion: Store binary content base64-encoded and decode it in the component
```

`lint` runs the same check (a referenced file or variable that can't be found is reported under `--strict`), so this kind
of problem is caught in CI, before any deployment.

Evaluating first has one consequence: on Docker, `${VAR}` in a shell member's config is also looked up **when the CLI
runs**, not when `docker compose` starts — the environment that runs `brickkit up` has to hold those variables.
