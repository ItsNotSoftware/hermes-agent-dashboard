#!/usr/bin/env python3
"""RPi Dashboard backend - serves system metrics and OpenAI plan usage data."""

import base64
import json
import os
import subprocess
import time
from http.server import HTTPServer, SimpleHTTPRequestHandler
from pathlib import Path
from datetime import datetime

PORT = 9200
DASHBOARD_DIR = Path(__file__).parent

# Read cron jobs
JOBS_PATH = Path.home() / '.hermes' / 'cron' / 'jobs.json'
AUTH_PATH = Path.home() / '.hermes' / 'auth.json'

# Cache OpenAI plan usage data
plan_cache = {'data': None, 'ts': 0}


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
        lines = result.stdout.strip().splitlines()[1:]
        procs = []
        hidden_exes = {'chromium', 'headless_shell'}
        for line in lines:
            parts = line.split(None, 10)
            if len(parts) >= 11:
                cmd = parts[10]
                # Strip path from executable, keep args
                tokens = cmd.split(None, 1)
                exe = tokens[0].split('/')[-1].lower()
                args = tokens[1] if len(tokens) > 1 else ''
                if exe in hidden_exes or 'chromium' in cmd.lower() or 'headless_shell' in cmd.lower():
                    continue
                name = (tokens[0].split('/')[-1] + ' ' + args).strip()[:45]
                cpu = float(parts[2])
                mem = float(parts[3])
                pid = parts[1]
                user = parts[0][:8]
                procs.append({'name': name, 'cpu': cpu, 'mem': mem, 'pid': pid, 'user': user})
                if len(procs) >= n:
                    break
        return procs
    except Exception:
        return []


def _read_hermes_model_config():
    """Read the top-level model config from ~/.hermes/config.yaml."""
    config_path = Path.home() / '.hermes' / 'config.yaml'
    model = ''
    provider = ''
    base_url = ''
    try:
        lines = config_path.read_text().splitlines()
    except Exception:
        return {'model': '', 'provider': '', 'base_url': ''}

    in_model_section = False
    for raw in lines:
        line = raw.rstrip()
        stripped = line.strip()
        if not stripped or stripped.startswith('#'):
            continue

        indent = len(line) - len(line.lstrip(' '))
        if indent == 0:
            if stripped == 'model:':
                in_model_section = True
                continue
            if in_model_section:
                break
            continue

        if not in_model_section or indent < 2:
            continue

        key, _, value = stripped.partition(':')
        value = value.strip().strip("'\"")
        if key == 'default':
            model = value
        elif key == 'provider':
            provider = value
        elif key == 'base_url':
            base_url = value

    return {'model': model, 'provider': provider, 'base_url': base_url}


def _infer_model_provider(model, provider='', base_url=''):
    """Return a display label for the model provider."""
    provider_l = (provider or '').strip().lower()
    model_l = (model or '').strip().lower()
    base_url_l = (base_url or '').strip().lower()

    # Respect an explicit provider when present.
    if provider_l in {'openai', 'openai-codex', 'codex'}:
        return 'OpenAI'
    if provider_l == 'openrouter':
        return 'OpenRouter'
    if provider_l == 'anthropic':
        return 'Anthropic'
    if provider_l == 'google':
        return 'Google'
    if provider_l == 'local':
        return 'Local'
    if provider_l == 'edge':
        return 'Edge'
    if provider_l and provider_l != 'auto':
        return provider.title()

    # Fall back to config hints / model naming.
    if 'chatgpt.com/backend-api/codex' in base_url_l or model_l.startswith('gpt-') or model_l.startswith('o1') or model_l.startswith('o3'):
        return 'OpenAI'
    if model_l.startswith('openrouter/'):
        return 'OpenRouter'
    if model_l.startswith('anthropic/') or 'claude' in model_l:
        return 'Anthropic'
    if model_l.startswith('google/') or 'gemini' in model_l:
        return 'Google'
    if model_l.startswith('local/'):
        return 'Local'

    return ''


def get_hermes_model_info():
    try:
        cfg = _read_hermes_model_config()
        model = cfg.get('model', '')
        if '/' in model:
            model = model.split('/')[-1]
        provider = _infer_model_provider(cfg.get('model', ''), cfg.get('provider', ''), cfg.get('base_url', ''))
        return {'model': model, 'provider': provider}
    except Exception:
        return {'model': '', 'provider': ''}


def get_hermes_model():
    return get_hermes_model_info().get('model', '')


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


def _decode_jwt_payload(token):
    try:
        payload = token.split('.')[1]
        payload += '=' * (-len(payload) % 4)
        return json.loads(base64.urlsafe_b64decode(payload))
    except Exception:
        return {}


def _load_openai_codex_state():
    try:
        auth_data = json.loads(AUTH_PATH.read_text())
        state = auth_data.get('providers', {}).get('openai-codex', {})
        tokens = state.get('tokens', {})
        access_token = str(tokens.get('access_token', '')).strip()
        refresh_token = str(tokens.get('refresh_token', '')).strip()
        if not access_token or not refresh_token:
            return None
        return {
            'auth_data': auth_data,
            'state': state,
            'access_token': access_token,
            'refresh_token': refresh_token,
        }
    except Exception:
        return None


def _save_openai_codex_tokens(auth_data, access_token, refresh_token):
    try:
        state = auth_data.setdefault('providers', {}).setdefault('openai-codex', {})
        tokens = state.setdefault('tokens', {})
        tokens['access_token'] = access_token
        tokens['refresh_token'] = refresh_token
        state['last_refresh'] = datetime.utcnow().isoformat() + 'Z'
        AUTH_PATH.write_text(json.dumps(auth_data, indent=2))
        os.chmod(AUTH_PATH, 0o600)
    except Exception as exc:
        print(f"OpenAI token save error: {exc}")


def _token_expiring(access_token, skew_seconds=120):
    claims = _decode_jwt_payload(access_token)
    exp = claims.get('exp')
    if not isinstance(exp, (int, float)):
        return False
    return time.time() >= (float(exp) - skew_seconds)


def _refresh_openai_codex_tokens(auth_state):
    import urllib.parse
    import urllib.request

    data = urllib.parse.urlencode({
        'grant_type': 'refresh_token',
        'refresh_token': auth_state['refresh_token'],
        'client_id': 'app_EMoamEEZ73f0CkXaXp7hrann',
    }).encode()
    req = urllib.request.Request(
        'https://auth.openai.com/oauth/token',
        data=data,
        headers={'Content-Type': 'application/x-www-form-urlencoded', 'Accept': 'application/json'},
    )
    with urllib.request.urlopen(req, timeout=15) as resp:
        payload = json.loads(resp.read())
    access_token = str(payload.get('access_token', '')).strip()
    refresh_token = str(payload.get('refresh_token', '')).strip() or auth_state['refresh_token']
    if not access_token:
        raise RuntimeError('Refresh did not return access_token')
    _save_openai_codex_tokens(auth_state['auth_data'], access_token, refresh_token)
    auth_state['access_token'] = access_token
    auth_state['refresh_token'] = refresh_token
    return access_token


def _get_openai_codex_access_token():
    auth_state = _load_openai_codex_state()
    if not auth_state:
        return None
    if _token_expiring(auth_state['access_token']):
        try:
            _refresh_openai_codex_tokens(auth_state)
        except Exception as exc:
            print(f"OpenAI token refresh error: {exc}")
    return auth_state


def _normalize_plan_usage(data):
    rate_limit = data.get('rate_limit') or {}
    primary = rate_limit.get('primary_window') or {}
    secondary = rate_limit.get('secondary_window') or {}
    return {
        'plan_type': str(data.get('plan_type', '')).strip() or 'unknown',
        'allowed': bool(rate_limit.get('allowed', False)),
        'limit_reached': bool(rate_limit.get('limit_reached', False)),
        'primary_window': {
            'used_percent': float(primary.get('used_percent', 0) or 0),
            'limit_window_seconds': int(primary.get('limit_window_seconds', 0) or 0),
            'reset_after_seconds': int(primary.get('reset_after_seconds', 0) or 0),
            'reset_at': int(primary.get('reset_at', 0) or 0),
        },
        'secondary_window': {
            'used_percent': float(secondary.get('used_percent', 0) or 0),
            'limit_window_seconds': int(secondary.get('limit_window_seconds', 0) or 0),
            'reset_after_seconds': int(secondary.get('reset_after_seconds', 0) or 0),
            'reset_at': int(secondary.get('reset_at', 0) or 0),
        },
        'code_review_rate_limit': data.get('code_review_rate_limit'),
        'credits': data.get('credits') or {},
    }


def fetch_openai_plan_usage():
    global plan_cache
    now = time.time()

    if plan_cache['data'] and (now - plan_cache['ts']) < 30:
        return plan_cache['data']

    auth_state = _get_openai_codex_access_token()
    if not auth_state:
        return None

    def _request_usage(access_token):
        req = urllib.request.Request(
            'https://chatgpt.com/backend-api/wham/usage',
            headers={
                'Authorization': f"Bearer {access_token}",
                'Accept': 'application/json',
            }
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read())

    import urllib.request
    import urllib.error
    try:
        data = _request_usage(auth_state['access_token'])
        plan_cache['data'] = _normalize_plan_usage(data)
        plan_cache['ts'] = now
        return plan_cache['data']
    except urllib.error.HTTPError as e:
        if e.code in (401, 403):
            try:
                fresh_token = _refresh_openai_codex_tokens(auth_state)
                data = _request_usage(fresh_token)
                plan_cache['data'] = _normalize_plan_usage(data)
                plan_cache['ts'] = now
                return plan_cache['data']
            except Exception as retry_exc:
                print(f"OpenAI plan retry error: {retry_exc}")
        print(f"OpenAI plan fetch error: {e}")
        return plan_cache['data']
    except Exception as e:
        print(f"OpenAI plan fetch error: {e}")
        return plan_cache['data']


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
        if self.path == '/api/model/info':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Access-Control-Allow-Origin', '*')
            self.end_headers()
            self.wfile.write(json.dumps(get_hermes_model_info()).encode())
            return
        if self.path == '/api/status':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Access-Control-Allow-Origin', '*')
            self.end_headers()
            
            plan_data = fetch_openai_plan_usage()
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
            
            model_info = get_hermes_model_info()
            status = {
                'hermes_model': model_info.get('model', ''),
                'model_info': model_info,
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
                # Show a couple more top processes in the dashboard widget.
                'procs': get_top_procs(7),
                'crons': get_cron_jobs(),
                'hostname': hostname,
                'kernel': kernel,
                'python': python_ver,
                'proc_count': proc_count,
                'threads': threads,
                'openai_plan': plan_data,
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
