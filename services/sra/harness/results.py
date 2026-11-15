"""Independent timed acceptance items: a failure never hides later items."""
import json
import time


class Results:
    def __init__(self):
        self.items = []
        self.values = {}

    def run(self, name, function):
        started = time.monotonic()
        try:
            value = function()
        except Exception as error:
            detail = f'{type(error).__name__}: {error}'
            if getattr(error, 'stderr', None):
                stderr = error.stderr
                if isinstance(stderr, bytes):
                    stderr = stderr.decode('utf-8', errors='replace')
                detail += '\n' + stderr[-800:]
            item = dict(name=name, passed=False, seconds=round(time.monotonic() - started, 2),
                        detail=detail)
        else:
            self.values[name] = value
            item = dict(name=name, passed=True, seconds=round(time.monotonic() - started, 2),
                        detail=value if isinstance(value, str) else '')
        self.items.append(item)
        print(f'{"PASS" if item["passed"] else "FAIL"} {name} {item["seconds"]:.2f}s'
              + (': ' + item['detail'].replace('\n', ' ') if item['detail'] else ''), flush=True)
        return self.values.get(name)

    def require(self, name):
        if name not in self.values:
            raise RuntimeError(f'prerequisite {name} failed; this item cannot execute')
        return self.values[name]

    def finish(self):
        print(json.dumps({'acceptance': self.items}), flush=True)
        return int(any(not item['passed'] for item in self.items))
