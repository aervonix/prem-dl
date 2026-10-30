# Prem-dl's YouTube challenge provider for yt-dlp, written into Prem-dl's data folder and loaded
# with --plugin-dirs. It hands the n/sig challenges to the solver Prem-dl keeps running
# (internal/jsc), which already has the player script parsed; yt-dlp's own solver parses it again
# on every run. Whenever Prem-dl can't answer, the request is declined and yt-dlp solves it itself.
import collections
import json
import os
import urllib.request

from yt_dlp.extractor.youtube.jsc.provider import (
    JsChallengeProvider,
    JsChallengeProviderError,
    JsChallengeProviderRejectedRequest,
    JsChallengeProviderResponse,
    JsChallengeResponse,
    JsChallengeType,
    NChallengeOutput,
    SigChallengeOutput,
    register_preference,
    register_provider,
)

_URL = os.environ.get('PREMDL_JSC_URL')
_TOKEN = os.environ.get('PREMDL_JSC_TOKEN')
# Straight to Prem-dl on this computer, never through a proxy from the environment.
_OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


@register_provider
class PremdlJCP(JsChallengeProvider):
    PROVIDER_VERSION = '1.0.0'
    PROVIDER_NAME = 'premdl'
    BUG_REPORT_LOCATION = 'https://github.com/aervonix/prem-dl/issues'
    _SUPPORTED_TYPES = [JsChallengeType.N, JsChallengeType.SIG]

    def is_available(self):
        return bool(_URL and _TOKEN)

    def _ask(self, body):
        req = urllib.request.Request(
            _URL, data=json.dumps(body).encode(), method='POST',
            headers={'Content-Type': 'application/json', 'Authorization': f'Bearer {_TOKEN}'})
        try:
            with _OPENER.open(req, timeout=30) as resp:
                return json.load(resp)
        except Exception as e:
            raise JsChallengeProviderRejectedRequest(f'Prem-dl did not answer: {e}', expected=True)

    def _real_bulk_solve(self, requests):
        grouped = collections.defaultdict(list)
        for request in requests:
            grouped[request.input.player_url].append(request)
        for player_url, batch in grouped.items():
            body = {
                'player_url': player_url,
                'requests': [{'type': r.type.value, 'challenges': r.input.challenges} for r in batch],
            }
            out = self._ask(body)
            if out.get('type') == 'need_player':
                video_id = next((r.video_id for r in batch if r.video_id), None)
                body['player'] = self._get_player(video_id, player_url)
                out = self._ask(body)
            if out.get('type') != 'result':
                raise JsChallengeProviderRejectedRequest(f'Prem-dl could not solve them: {out.get("error")}', expected=True)
            for request, answer in zip(batch, out['responses'], strict=True):
                if answer.get('type') != 'result':
                    yield JsChallengeProviderResponse(request, None, JsChallengeProviderError(answer.get('error'), expected=True))
                    continue
                output = (NChallengeOutput if request.type is JsChallengeType.N else SigChallengeOutput)(answer['data'])
                yield JsChallengeProviderResponse(request, JsChallengeResponse(request.type, output))


@register_preference(PremdlJCP)
def _premdl_first(provider, requests):
    # Ahead of yt-dlp's own providers, which start a JavaScript runtime for every run.
    return 10000
