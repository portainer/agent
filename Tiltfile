local_resource(
  'build_agent',
  cmd="make agent",
  deps=['*.go', 
    'agent.go',
    'build',
    'chisel',
    'cmd',
    'config',
    'constants',
    'crypto',
    'dist',
    'docker',
    'edge',
    'exec',
    'filesystem',
    'ghw',
    'healthcheck',
    'http',
    'internals',
    'kubernetes',
    'net',
    'nomad',
    'os',
    'release.sh',
    'serf',
    'static'
  ],
  ignore=[
    'dist', 
    'tilt',
    '/**/*_test.go'
  ],
)

docker_compose('docker-compose.yaml')

docker_build('portainer-agent', 
  '.',
  only = ['dist', 'build', 'static', 'config'],
  dockerfile='build/linux/alpine.Dockerfile',
  live_update = [
    sync('./dist', '/app'),
    sync('./static', '/app/static'),
    restart_container()
  ])
