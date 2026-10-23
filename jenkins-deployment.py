#!/usr/bin/env python3
"""Submit or observe the exact pushed CLI revision through Jenkins."""
import os
from pathlib import Path
import re
import subprocess
import sys


def main():
    revision = os.environ.get('DEPLOY_IT_COMMIT', '')
    if (not re.fullmatch('[0-9a-f]{40}', revision)
            or os.environ.get('DEPLOY_IT_BRANCH') != 'main'
            or os.environ.get('DEPLOY_IT_ENVIRONMENT') != 'local'):
        raise RuntimeError('Expected the exact pushed deploy-it main revision for the configured local Mac')
    helper = Path.home() / '.local/share/jenkins-local/one-shot-tally-delivery.py'
    subprocess.run([sys.executable, str(helper), 'deploy', '--profile', 'deploy-it',
                    '--revision', revision], check=True)



if __name__ == '__main__':
    main()
