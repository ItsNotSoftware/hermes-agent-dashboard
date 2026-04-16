# hermes-agent-dashboard

A lightweight dashboard for monitoring a [Hermes](https://github.com/nousresearch/hermes-agent) agent running on a Raspberry Pi. Displays real-time system metrics, OpenRouter API usage, cron jobs, and top processes — designed for an 800×480 touchscreen.

![Dashboard](dashboard.png)

## Features

- CPU usage (per-core + aggregate), temperature, and frequency
- RAM, disk, and swap usage
- Network throughput (up/down)
- Disk I/O (read/write)
- OpenRouter spend tracking (daily, weekly, monthly + 7-day sparkline)
- Hermes cron job status
- Top processes (htop-style)

## Usage

The backend reads the OpenRouter API key from `~/.hermes/.env`:

Launch the dashboard:

```bash
bash launch.sh
```

