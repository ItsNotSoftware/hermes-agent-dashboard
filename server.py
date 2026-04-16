#!/usr/bin/env python3
"""RPi Dashboard backend - serves system metrics and OpenRouter usage data."""

import json
import os
import subprocess
import time
from http.server import HTTPServer, SimpleHTTPRequestHandler
from pathlib import Path
from datetime import datetime, timedelta

PORT = 9200
DASHBOARD_DIR = Path(__file__).parent

# Get OpenRouter API key
ENV_PATH = Path.home() / '.hermes' / '.env'
API_KEY = ''
try:
    for line in ENV_PATH.read_text().splitlines():
        if line.startswith('OPENROUTER_API_KEY=') and not line.strip().startswith('#'):
            API_KEY = line.split('=', 1)[1].strip().strip("'\"")
            break
except Exception:
    pass

# Read cron jobs
JOBS_PATH = Path.home() / '.hermes' / 'cron' / 'jobs.json'

# Cache OpenRouter data
or_cache = {'data': None, 'ts': 0, 'daily_history': []}


def get_temp():
    try:
        t = Path('/sys/class/thermal/thermal_zone0/temp').read_text().strip()
        return float(t) / 1000
    except Exception:
        return 0.0


_cpu_prev = {}  # key -> (total, idle)

def _read_proc_stat():
    """Return dict of cpu_name -> (total_jiffies, idle_jiffies)."""
    result = {}
    with open('/proc/stat') as f:
        for line in f:
            if not line.startswith('cpu'):
                break
            parts = line.split()
            name = parts[0]
            vals = list(map(int, parts[1:]))
            # idle = idle + iowait (index 3, 4)
            idle = vals[3] + (vals[4] if len(vals) > 4 else 0)
            total = sum(vals)
            result[name] = (total, idle)
    return result

def get_cpu_usage():
    """Return {'total': aggregate_pct, 'max_core': highest_single_core_pct, 'cores': [per_core_pct]}."""
    global _cpu_prev
    try:
        curr = _read_proc_stat()
        if not _cpu_prev:
            _cpu_prev = curr
            return {'total': 0.0, 'max_core': 0.0, 'cores': []}

        def pct(name):
            if name not in curr or name not in _cpu_prev:
                return 0.0
            dt = curr[name][0] - _cpu_prev[name][0]
            di = curr[name][1] - _cpu_prev[name][1]
            return max(0.0, 100.0 * (1 - di / dt)) if dt > 0 else 0.0

        total = pct('cpu')
        core_names = sorted([k for k in curr if k != 'cpu'])
        cores = [round(pct(k), 1) for k in core_names]
        max_core = max(cores) if cores else total
        _cpu_prev = curr
        return {'total': round(total, 1), 'max_core': round(max_core, 1), 'cores': cores}
    except Exception:
        return {'total': 0.0, 'max_core': 0.0, 'cores': []}


def get_memory_and_swap():
    try:
        result = subprocess.run(['free', '-m'], capture_output=True, text=True, timeout=5)
        lines = result.stdout.strip().splitlines()
        mem = lines[1].split()
        swp = lines[2].split()
        return (
            {'total': float(mem[1]), 'used': float(mem[2])},
            {'total': float(swp[1]), 'used': float(swp[2])},
        )
    except Exception:
        return {'total': 0, 'used': 0}, {'total': 0, 'used': 0}


def get_disk():
    try:
        stat = os.statvfs('/')
        total = (stat.f_blocks * stat.f_frsize) / (1024**3)
        free = (stat.f_bavail * stat.f_frsize) / (1024**3)
        return {'total': total, 'used': total - free}
    except Exception:
        return {'total': 0, 'used': 0}


def get_cpu_freq():
    try:
        freq_str = Path('/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq').read_text().strip()
        return int(freq_str) // 1000
    except Exception:
        try:
            result = subprocess.run(['vcgencmd', 'measure_clock', 'arm'], capture_output=True, text=True, timeout=5)
            freq = int(result.stdout.strip().split('=')[1])
            return freq // 1000000 if freq > 100000000 else 0
        except Exception:
            return 0


def get_gpu_mem():
    for p in ['/boot/firmware/config.txt', '/boot/config.txt']:
        try:
            for line in Path(p).read_text().splitlines():
                if line.startswith('gpu_mem'):
                    return int(line.split('=')[1])
        except Exception:
            continue
    return 0


def get_uptime():
    try:
        return float(Path('/proc/uptime').read_text().split()[0])
    except Exception:
        return 0


def get_load():
    try:
        return list(os.getloadavg())
    except Exception:
        return [0, 0, 0]


_net_prev = {'rx': 0, 'tx': 0, 'ts': 0.0}

def get_network():
    global _net_prev
    rx = 0
    tx = 0
    try:
        with open('/proc/net/dev') as f:
            for line in f:
                if 'eth' in line or 'wlan' in line:
                    parts = line.split(':')[1].split()
                    rx += int(parts[0])
                    tx += int(parts[8])
    except Exception:
        pass

    now = time.time()
    rx_speed = 0.0
    tx_speed = 0.0
    if _net_prev['ts'] > 0:
        dt = now - _net_prev['ts']
        if dt > 0:
            rx_speed = max(0.0, (rx - _net_prev['rx']) / dt)
            tx_speed = max(0.0, (tx - _net_prev['tx']) / dt)

    _net_prev['rx'] = rx
    _net_prev['tx'] = tx
    _net_prev['ts'] = now
    return {'rx': rx_speed, 'tx': tx_speed}


_disk_prev = {'rd': 0, 'wt': 0, 'ts': 0.0}

def get_disk_io():
    """Return current disk read/write speed in bytes/sec."""
    global _disk_prev
    rd = 0
    wt = 0
    try:
        with open('/proc/diskstats') as f:
            for line in f:
                if 'mmcblk0 ' in line or 'sda ' in line:
                    parts = line.split()
                    rd += int(parts[5]) * 512
                    wt += int(parts[9]) * 512
    except Exception:
        pass

    now = time.time()
    rd_speed = 0.0
    wt_speed = 0.0
    if _disk_prev['ts'] > 0:
        dt = now - _disk_prev['ts']
        if dt > 0:
            rd_speed = max(0.0, (rd - _disk_prev['rd']) / dt)
            wt_speed = max(0.0, (wt - _disk_prev['wt']) / dt)

    _disk_prev['rd'] = rd
    _disk_prev['wt'] = wt
    _disk_prev['ts'] = now
    return {'rd': rd_speed, 'wt': wt_speed}


def get_top_procs(n=10):
    try:
        result = subprocess.run(['ps', 'aux', '--sort=-%cpu'], capture_output=True, text=True, timeout=5)
        lines = result.stdout.strip().splitlines()[1:n+1]
        procs = []
        for line in lines:
            parts = line.split(None, 10)
            if len(parts) >= 11:
                cmd = parts[10]
                # Strip path from executable, keep args
                tokens = cmd.split(None, 1)
                exe = tokens[0].split('/')[-1]
                args = tokens[1] if len(tokens) > 1 else ''
                name = (exe + ' ' + args).strip()[:45]
                cpu = float(parts[2])
                mem = float(parts[3])
                pid = parts[1]
                user = parts[0][:8]
                procs.append({'name': name, 'cpu': cpu, 'mem': mem, 'pid': pid, 'user': user})
        return procs
    except Exception:
        return []


def get_hermes_model():
    try:
        config_path = Path.home() / '.hermes' / 'config.yaml'
        for line in config_path.read_text().splitlines():
            line = line.strip()
            if line.startswith('default:'):
                model = line.split(':', 1)[1].strip()
                return model.split('/')[-1] if '/' in model else model
    except Exception:
        pass
    return ''


def get_cron_jobs():
    """Read cron jobs from Hermes cron state file."""
    if not JOBS_PATH.exists():
        return []
    try:
        data = json.loads(JOBS_PATH.read_text())
        jobs = data.get('jobs', [])
        result = []
        now = datetime.now()
        for j in jobs:
            # Extract schedule info
            schedule = j.get('schedule', {})
            if isinstance(schedule, dict):
                kind = schedule.get('kind', 'cron')
                if kind == 'cron':
                    schedule_str = schedule.get('display', schedule.get('expr', ''))
                elif kind == 'once':
                    run_at = schedule.get('run_at', '')
                    schedule_str = 'once: ' + run_at[:16] if run_at else 'once'
                else:
                    schedule_str = schedule.get('display', str(schedule))
            else:
                schedule_str = str(schedule)
            
            # Determine state
            raw_state = j.get('state', 'scheduled')
            if raw_state == 'paused':
                display_state = 'paused'
            else:
                display_state = 'active'
            
            # Next run time
            next_run_at = j.get('next_run_at', '')
            if next_run_at:
                try:
                    dt = datetime.fromisoformat(next_run_at.replace('Z', '+00:00'))
                    dt = dt.astimezone().replace(tzinfo=None)  # convert to local time
                    next_str = dt.strftime('%d/%m - %H:%M')
                except Exception:
                    next_str = next_run_at[:19]
            else:
                next_str = 'N/A'
            
            job_info = {
                'id': j.get('id', ''),
                'name': j.get('name', 'Unnamed'),
                'schedule': schedule_str,
                'state': display_state,
                'next_run': next_str,
                'next_run_at': j.get('next_run_at', ''),
                'last_status': j.get('last_status', None),
                'model': j.get('model', ''),
            }
            result.append(job_info)

        # Sort by next_run_at ISO string (naturally sortable)
        result.sort(key=lambda x: x.get('next_run_at', 'zzz'))
        return result
    except Exception as e:
        print(f"Error reading cron jobs: {e}")
        return []


def fetch_openrouter():
    global or_cache
    now = time.time()
    
    # Cache for 60 seconds
    if or_cache['data'] and (now - or_cache['ts']) < 60:
        return or_cache['data'], or_cache['daily_history']
    
    if not API_KEY:
        return None, []
    
    import urllib.request
    try:
        req = urllib.request.Request(
            'https://openrouter.ai/api/v1/auth/key',
            headers={'Authorization': f'Bearer {API_KEY}'}
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
        
        result_data = data.get('data', {})
        
        # Try daily usage endpoint first, fall back gracefully
        daily_history = []
        try:
            today = datetime.now()
            for i in range(7, 0, -1):
                d = today - timedelta(days=i)
                date_str = d.strftime('%Y-%m-%d')
                url = f'https://openrouter.ai/api/v1/usage/date-statistics?start={date_str}&end={date_str}'
                req2 = urllib.request.Request(url, headers={'Authorization': f'Bearer {API_KEY}'})
                resp2 = urllib.request.urlopen(req2, timeout=10)
                raw = resp2.read()
                # Check if it's JSON (not HTML 404)
                try:
                    usage_data = json.loads(raw)
                    total_cost = 0
                    if 'data' in usage_data and 'models' in usage_data['data']:
                        for model in usage_data['data']['models']:
                            total_cost += model.get('total_cost', 0)
                    daily_history.append({
                        'label': d.strftime('%a'),
                        'val': round(total_cost, 4),
                    })
                except json.JSONDecodeError:
                    # Endpoint returned HTML (404), use zero
                    daily_history.append({'label': d.strftime('%a'), 'val': 0})
        except Exception:
            # Endpoint failed entirely, create zeros from auth/key daily
            daily = result_data.get('usage_daily', 0)
            daily_history = [{'label': d.strftime('%a'), 'val': 0}
                           for d in [datetime.now() - timedelta(days=i) for i in range(7, 0, -1)]]
            # Set today as the last entry
            daily_history[-1]['val'] = round(daily, 4)
        
        or_cache['data'] = result_data
        or_cache['daily_history'] = daily_history
        or_cache['ts'] = now
        
        return result_data, daily_history
    except Exception as e:
        print(f"OpenRouter fetch error: {e}")
        return or_cache['data'], or_cache.get('daily_history', [])


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=str(DASHBOARD_DIR), **kwargs)
    
    def do_POST(self):
        if self.path == '/api/exit':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"status": "exiting"}')
            # Kill chromium kiosk windows
            import subprocess as _sp
            _sp.Popen(['pkill', '-f', 'chromium.*9200'], stdout=_sp.DEVNULL, stderr=_sp.DEVNULL)
            return
        super().do_GET()  # 404 for other POSTs
    
    def do_GET(self):
        if self.path == '/api/exit':
            # Support GET-based exit for browsers that can't POST
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(b'{"status": "exiting"}')
            import subprocess as _sp
            _sp.Popen(['pkill', '-f', 'chromium.*9200'], stdout=_sp.DEVNULL, stderr=_sp.DEVNULL)
            return
        if self.path == '/api/status':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Access-Control-Allow-Origin', '*')
            self.end_headers()
            
            or_data, daily_hist = fetch_openrouter()
            net = get_network()
            disk_io = get_disk_io()
            memory, swap = get_memory_and_swap()
            
            # System info
            hostname = ''
            kernel = ''
            python_ver = ''
            try:
                hostname = subprocess.run(['hostname'], capture_output=True, text=True, timeout=2).stdout.strip()
                kernel = subprocess.run(['uname', '-r'], capture_output=True, text=True, timeout=2).stdout.strip()
                python_ver = subprocess.run(['python3', '--version'], capture_output=True, text=True, timeout=2).stdout.strip()
            except Exception:
                pass
            
            proc_count = 0
            threads = 0
            try:
                result = subprocess.run(['ps', 'aux', '--no-headers'], capture_output=True, text=True, timeout=3)
                proc_count = len(result.stdout.strip().splitlines())
                with open('/proc/loadavg') as f:
                    threads = int(f.read().split()[3].split('/')[1])
            except Exception:
                pass
            
            status = {
                'hermes_model': get_hermes_model(),
                'temp': get_temp(),
                'cpu': get_cpu_usage(),  # {'total': %, 'max_core': %}
                'memory': memory,
                'swap': swap,
                'disk': get_disk(),
                'cpuFreq': get_cpu_freq(),
                'uptime': get_uptime(),
                'netRx': net['rx'],
                'netTx': net['tx'],
                'diskRd': disk_io.get('rd', 0),
                'diskWt': disk_io.get('wt', 0),
                'procs': get_top_procs(5),
                'crons': get_cron_jobs(),
                'hostname': hostname,
                'kernel': kernel,
                'python': python_ver,
                'proc_count': proc_count,
                'threads': threads,
                'or': {
                    'data': or_data,
                    'daily_history': daily_hist,
                } if or_data else None,
            }
            
            self.wfile.write(json.dumps(status).encode())
        else:
            super().do_GET()
    
    def log_message(self, format, *args):
        pass  # Suppress logging


if __name__ == '__main__':
    server = HTTPServer(('0.0.0.0', PORT), Handler)
    print(f"Dashboard server running on http://0.0.0.0:{PORT}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        server.shutdown()
