# View logs

## Find a run

Go to the repo's **Pipelines** tab. You'll see all workflows defined in `.github/workflows/`, each showing its last run status and when it ran.

Click a workflow to see its run history. Click a specific run to open it.

## Read the run detail

A run is made up of jobs. Each job has steps. Click a job to expand it and see step-by-step output.

Logs stream in real time while a run is in progress. You don't need to refresh.

## Run statuses

| Status | Meaning |
|--------|---------|
| **Pending** | Queued, waiting for a runner |
| **Running** | Currently executing |
| **Success** | All jobs passed |
| **Failure** | At least one job failed |
| **Cancelled** | Manually cancelled before completion |

## Search logs

Use Cmd+F (Mac) or Ctrl+F (Windows/Linux) to search within the log output for a specific string or error message.

## Cancel a run

While a run is **Running** or **Pending**, click **Cancel** in the top right of the run detail page. Steps that have already completed stay completed; the rest are stopped.

## Re-run

After a failure, click **Re-run jobs** to retry. You can re-run all jobs or only the failed ones.

Useful when a step failed due to a flaky network call or a transient infrastructure issue rather than a code problem.

## Logs retention

Run logs are kept for 90 days by default. After that, the run record remains (status, timestamps, commit SHA) but the raw log output is deleted.

## Debug mode

To get verbose output from all steps, re-run with **Enable debug logging**. This sets `RUNNER_DEBUG=1` in the run environment, which many actions use to print extra detail.
