# iOS Shortcut

This guide builds a Shortcut that sends a post link from the share sheet to your OpenMediaDownloaderIOS server and saves the downloaded media on the device. You need a running server, as described in [Self-Hosting](self-hosting.md), and an API key created on it.

## Example shortcut

An example shortcut with placeholder values will be linked here. After adding it, open it in the Shortcuts app and replace the two placeholders described below. Keep your API key private, and do not share a copy of the shortcut that contains it.

## Placeholders

The shortcut starts with two **Text** actions, each saved to a variable with **Set Variable**:

| Variable | Value |
| --- | --- |
| `Server` | The server address without a trailing slash, the same value as `OMDI_PUBLIC_URL`, for example `https://media.example.com`. |
| `API Key` | The key printed by `omdi keys create`, in the form `omdi_<id>_<secret>`. |

## Build it step by step

Create a new shortcut and add these actions in order. Action names are those shown in the Shortcuts app in English.

### 1. Choose a new download or resume an existing job

Configure the shortcut to **Receive** URLs and Text **from** Share Sheet. Set **If there's no input** to **Continue**, so running it directly can also resume a job without asking for a new post link.

After the `Server` and `API Key` variables, add **Choose from Menu** with two options: `New download` and `Resume download`. Put the actions below inside their respective menu branches. Both branches set `Job ID`; the polling and saving actions go after **End Menu**, in this same shortcut.

### 2. Obtain the job ID

#### New download branch

1. **If** Shortcut Input has any value, save it as `Post Input`. **Otherwise**, **Ask for Input** of type Text with the prompt `Post URL`, and save the answer as `Post Input`. **End If**.
2. **Get URLs from** `Post Input`. The share sheet of some apps sends text that contains the link; this action extracts it.
3. **Get Item from List**: First Item of URLs. Save it with **Set Variable** `Post`.
4. **Get Contents of URL** with `Server`/v1/jobs, and expand the options:
   - **Method:** POST.
   - **Headers:** `Authorization` = `Bearer ` followed by the `API Key` variable.
   - **Request Body:** JSON, with one Text field `url` = `Post`.
5. Save the response as `Creation Response`.
6. **Get Dictionary Value** for `error` in `Creation Response`.
7. **If** Dictionary Value has any value: **Show Alert** with that error, then **Stop This Shortcut**. **End If**. This preserves the original error, such as `unsupported_url` or `too_many_jobs`, and prevents polling without a job.
8. **Get Dictionary Value** for `id` in `Creation Response`. Save it as `Job ID`.

#### Resume download branch

1. **Ask for Input** of type Text with the prompt `Existing job ID`. Paste the ID saved when the previous polling window ended.
2. Save the answer as `Job ID`. This branch sends no POST request.

After **End Menu**, add **If** `Job ID` does not have any value: **Show Alert** `No job ID was provided`, then **Stop This Shortcut**. **End If**. Continue with the shared actions below, using the same server and API key that created the job.

### 3. Wait for the job to finish

Shortcuts has no loop that runs until a condition is met, so the shortcut polls a bounded number of times and skips the remaining iterations once the job has finished.

1. **Text** `waiting`, saved as `State`.
2. **Repeat** 200 times:
   1. **If** `State` is `waiting`:
      1. **Wait** 3 seconds.
      2. **Get Contents of URL** with `Server`/v1/jobs/`Job ID`, method GET, and the same `Authorization` header.
      3. Save the result as `Job`.
      4. **Get Dictionary Value** for `status` in `Job`.
      5. **If** Dictionary Value does not have any value: **Get Dictionary Value** for `error` in `Job`, **Show Alert** with that error and `Job ID`, then **Stop This Shortcut**. **End If**. For example, `not_found` can mean the ID is wrong, belongs to another key, or the job has been removed.
      6. **Get Dictionary Value** for `status` in `Job` again. **If** it is not `queued`, and is not `running`: **Text** `done`, saved as `State`.
   2. **End If**.
3. **End Repeat**.

Two hundred polls three seconds apart provide about ten minutes of waiting, plus request time. The server's default ten-minute job timeout starts only when the single worker takes the job; time spent queued is additional. A healthy job can therefore still be queued or running when this polling window ends. In that case, preserve its ID and use `Resume download` as described below.

### 4. Save the media

1. **Get Dictionary Value** for `status` in `Job`.
2. **If** Dictionary Value is `succeeded`:
    1. **Get Dictionary Value** for `items` in `Job`.
    2. **Repeat with Each** item in Dictionary Value:
       1. **Get Dictionary Value** for `download_url` in Repeat Item, then **Get Contents of URL** with it. No API key is needed: the link itself is the credential.
       2. **Get Dictionary Value** for `media_type` in Repeat Item.
       3. **If** it begins with `audio/`: **Save File** with Contents of URL. **Otherwise:** **Save to Photo Album** with Contents of URL.
    3. **End Repeat**.
3. **Otherwise:** add the status handling described in [Failures](#failures).

A carousel produces several items, saved in order. Download links expire after 15 minutes by default, and every poll replaces the previous ones, so the shortcut downloads the items right after the last poll.

## Failures

When the final status is not `succeeded`, show the reason with **Show Alert**:

- `failed`: show the `error` value and, when present, `error_detail`. For example, `extraction_failed` with `login_required` means the platform does not serve that post to anonymous visitors, and `unavailable` means the post is private, removed, or does not exist. The [README](../README.md#jobs-and-downloads) lists every value.
- `canceled`: the job was canceled on the server.
- Still `queued` or `running`: **Copy to Clipboard** `Job ID`, then **Show Alert** with `Job ID` and the message `This job is still pending. Its ID has been copied. Run this shortcut again, choose Resume download, and paste the ID.` Follow it with **Stop This Shortcut**. Save the ID somewhere durable if you will resume later, since the clipboard can be overwritten. Selecting `Resume download` queries the existing job; selecting `New download` submits another one. Resume before the job's retention expires (24 hours by default); a successful job then returns fresh download links.

If creating the job fails, the response's `error` explains why: `unauthorized` means the API key is missing, wrong, or revoked; `unsupported_url` means the link is not one public post on a supported platform; and `too_many_jobs` means the key already has the maximum number of active jobs.

Every response carries an `X-Trace-Id` header. When reporting a problem to the server administrator, the job ID and the time are usually enough for them to find it in the logs.
