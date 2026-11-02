import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from plugin import home_assistant

calls = []
class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args): pass
    def do_GET(self):
        calls.append((self.command, self.path, self.headers.get('Authorization')))
        body = (Path(__file__).parent / 'testdata/states.json').read_bytes() if self.path == '/api/states' else b'{}'
        self.send_response(200); self.end_headers(); self.wfile.write(body)
    def do_POST(self):
        calls.append((self.command, self.path, json.loads(self.rfile.read(int(self.headers['Content-Length'])))))
        self.send_response(200); self.end_headers(); self.wfile.write(b'[]')

class HomeAssistantTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever); cls.thread.start()
    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown(); cls.server.server_close(); cls.thread.join()
    def setUp(self):
        calls.clear()
        self.send('/ether/configure', {'url': 'http://127.0.0.1:%d' % self.server.server_port, 'token': 'test-token', 'started': 'scene.turn_on scene.movie'})
    def send(self, path, body):
        return home_assistant.handle({'method': 'POST', 'path': path, 'body': json.dumps(body)})
    def test_events_and_tools(self):
        result = self.send('/ether/event', {'events': [{'id':'one','name':'playback.started'}, {'id':'two','name':'playback.paused'}]})
        self.assertEqual(result['body']['ack'], ['one', 'two'])
        self.assertEqual(calls[0], ('POST', '/api/services/scene/turn_on', {'entity_id':'scene.movie'}))
        result = self.send('/ether/tool/ha_states', {})
        self.assertEqual(result['body']['states'][0], {'entity_id':'light.lounge','state':'on'})
        self.assertEqual(calls[1][2], 'Bearer test-token')
        self.assertEqual(self.send('/ether/tool/ha_run_scene', {'entity_id':'scene.movie'})['body'], {'ok':True})
    def test_invalid_mapping_is_not_acknowledged(self):
        self.send('/ether/configure', {'url':'http://127.0.0.1:%d' % self.server.server_port, 'token':'test-token', 'started':'bad'})
        self.assertEqual(self.send('/ether/event', {'events':[{'id':'x','name':'playback.started'}]})['body']['ack'], [])
        self.assertEqual(calls, [])

if __name__ == '__main__': unittest.main()
