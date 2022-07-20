# Honeycomb AWS Changelog

## 1.4.4 2022-07-20

### Maintenance

- fix openSSL CVE by re-releasing on a new container | [@kentquirk](https://github.com/kentquirk)

## 1.4.3 2022-04-19

### Maintenance

- update go to 1.18 (#186)| [@MikeGoldsmith](https://github.com/MikeGoldsmith)
  - fixes openSSL CVE (#185)
- Fix readme instructions and add terraform snippet (#184) | [@martin308](https://github.com/martin308)
- gh: add re-triage workflow (#176) | [@vreynolds](https://github.com/vreynolds)
- Update dependabot.yml (#173) | [@vreynolds](https://github.com/vreynolds)
- Update awsclient orb (#182) | [@MikeGoldsmith](https://github.com/MikeGoldsmith)
- Bump github.com/honeycombio/honeytail from 1.5.0 to 1.6.1 (#169, #179)
- Bump github.com/honeycombio/libhoney-go from 1.15.6 to 1.15.8 (#180)
- Bump github.com/aws/aws-sdk-go from 1.41.5 to 1.43.31 (#172, #174, #175, #177, #187)

## 1.4.2 2021-11-05

- bump go version to 1.17 (#167)
- bump libhoney-go (#166)
- empower apply-labels action to apply labels (#165)
- Bump github.com/honeycombio/libhoney-go from 1.15.4 to 1.15.5 (#150)
- Bump github.com/aws/aws-sdk-go from 1.40.47 to 1.41.5 (#161)
- Typo in publish_docker job (#160)

## 1.4.1 2021-10-13

### Added

- Build and publish multi-arch docker images on tag (#153) | [@MikeGoldsmith](https://github.com/MikeGoldsmith)

### Fixes

- Fix building binaries commands so they pick up the GOOS and GOARCH vars (#97) | [@vreynolds](https://github.com/vreynolds)
- Login to docker for publish_docker (#159) | [@jamiedanielson](https://github.com/jamiedanielson)

### Maintenance

- Change maintenance badge to maintained (#148)
- Adds Stalebot (#149)
- Bump github.com/aws/aws-sdk-go from 1.40.28 to 1.40.47 (#147)
- Bump github.com/honeycombio/honeytail from 1.3.0 to 1.5.0 (#116)
- Bump github.com/jessevdk/go-flags from 1.4.0 to 1.5.0 (#107)
- Add NOTICE (#143)
- Bump github.com/aws/aws-sdk-go from 1.38.12 to 1.40.28 (#140)
- Add OSS lifecycle badge (#138)
- Add community health files (#137)
- Bump github.com/honeycombio/libhoney-go from 1.15.2 to 1.15.4 (#133)
- Updates Github Action Workflows (#128)
- Updates Dependabot Config (#126)
- Switches CODEOWNERS to telemetry-team (#125)
- move apply-labels under workflows, so it runs (#110)
- Bump github.com/sirupsen/logrus from 1.8.0 to 1.8.1 (#104)
- Bump github.com/aws/aws-sdk-go from 1.37.15 to 1.38.12 (#109)
- Bump github.com/honeycombio/honeytail from 1.2.0 to 1.3.0 (#98)
- Bump github.com/aws/aws-sdk-go from 1.37.6 to 1.37.15 (#101)
- Bump github.com/sirupsen/logrus from 1.7.0 to 1.8.0 (#100)
- Build amd64, arm64 binaries for linux and darwin. (#96)
- Bump github.com/aws/aws-sdk-go from 1.37.3 to 1.37.6 (#95)
