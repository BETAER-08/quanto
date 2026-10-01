# Creating the quanto GitHub App

quanto runs as a GitHub App. The `quanto manifest` command prints a [GitHub App manifest](https://docs.github.com/en/apps/sharing-github-apps/registering-a-github-app-from-a-manifest) with the permissions and events quanto needs, so the App can be registered without filling in the settings form by hand.

## 1. Generate the manifest

Choose the public URL where GitHub will deliver webhooks (the `/webhook` path of the web role behind your TLS reverse proxy, see [deploy.md](deploy.md)) and a homepage URL.

```sh
quanto manifest \
  --webhook-url https://quanto.example.com/webhook \
  --homepage-url https://quanto.example.com \
  --name quanto > manifest.json
```

`--name` defaults to `quanto`. GitHub App names are unique across GitHub, so pick another name if `quanto` is taken. Both URLs must be absolute `http` or `https` URLs.

The manifest requests:

| Permission | Access |
|---|---|
| `actions` | read |
| `checks` | write |
| `contents` | read |
| `metadata` | read |
| `pull_requests` | write |

and subscribes to the `pull_request` and `workflow_run` events. `installation` and `installation_repositories` events are delivered to every GitHub App without a subscription.

## 2. Submit the manifest to GitHub

GitHub accepts a manifest through an HTML form posted from your browser. Write a local HTML file that contains the manifest:

```sh
{
  printf '<form action="https://github.com/settings/apps/new" method="post">\n'
  printf '<textarea name="manifest" rows="30" cols="80">'
  cat manifest.json
  printf '</textarea>\n<button type="submit">Create GitHub App</button>\n</form>\n'
} > create-app.html
```

To create the App under an organization instead of your personal account, use `https://github.com/organizations/<org>/settings/apps/new` as the form action.

Open `create-app.html` in a browser where you are signed in to GitHub, press **Create GitHub App**, review the name on the GitHub page, and confirm.

## 3. Collect the credentials

Open the App settings page: **Settings → Developer settings → GitHub Apps → \<name\>** (for an organization: **Organization settings → Developer settings → GitHub Apps**).

1. **App ID.** Shown under *About*. This value becomes `QUANTO_APP_ID`.
2. **Webhook secret.** Generate a random value, for example with `openssl rand -hex 32`, enter it in the *Webhook secret* field, and save. quanto rejects every delivery whose `X-Hub-Signature-256` does not match this secret.
3. **Private key.** Under *Private keys*, press **Generate a private key**. GitHub downloads a `.pem` file. quanto accepts both PKCS#1 and PKCS#8 PEM keys.

Store the three values as described in [deploy.md](deploy.md). Delete the downloaded key file after it has been stored as a Podman secret.

## 4. Install the App

Open `https://github.com/apps/<app-slug>` and press **Install**. Choose the account and the repositories quanto should analyze.

When the installation is created, quanto records the installation and its repositories and backfills recent completed workflow runs for each repository, which feed the runner-minute estimates. Repositories added to the installation later are backfilled the same way.

Private repositories are ignored unless `QUANTO_ALLOW_PRIVATE_REPOS=true` is set on the web and worker processes.

From then on, every pull request that is opened, reopened, or synchronized and that changes `.github/workflows/*.yml` or `.github/workflows/*.yaml` receives a `quanto` Check Run with a `neutral` conclusion, and a summary comment when the change contains findings of normal or high significance.
