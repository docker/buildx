
@Library('jps')
_

pipeline {
  agent {
    node {
      label 'ubuntu-1804-overlay2'
    }
  }
  options {
    disableConcurrentBuilds()
  }
  stages {
    stage("FOSSA Analyze") {
      steps {

        withCredentials([string(credentialsId: 'fossa-api-key', variable: 'FOSSA_API_KEY')]) {
          withGithubStatus('FOSSA.scan') {
            labelledShell returnStatus: false, returnStdout: true, label: "make fossa-analyze",
                  script:'make -f Makefile.fossa BRANCH_NAME=${BRANCH_NAME} fossa-analyze'
            labelledShell returnStatus: false, returnStdout: true, label: "make fossa-test",
                  script: 'make -f Makefile.fossa BRANCH_NAME=${BRANCH_NAME} fossa-test'
          }
        }
      }
    }
  }
}
