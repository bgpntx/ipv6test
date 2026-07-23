pipeline {
    agent any

    options {
        disableConcurrentBuilds()
    }

    environment {
        // Server configuration
        DEPLOY_HOST   = "0ms.app"
        DEPLOY_PORT   = "2562"
        DEPLOY_USER   = "root"
        DEPLOY_PATH   = "/root/git/ip6"
        DEPLOY_BRANCH = "main"

        DISCORD_URL   = credentials('discord_bug')
    }

    stages {
        stage('Check Changed Files') {
            steps {
                script {
                    // Перевірити чи змінено критичні файли
                    def criticalFilesChanged = sh(
                        script: '''
                            # Отримати список змінених файлів
                            CHANGED_FILES=$(git diff --name-only HEAD~1 HEAD 2>/dev/null || echo "")

                            if [ -z "$CHANGED_FILES" ]; then
                                echo "No changes detected (possibly first build)"
                                exit 1  # Рестартувати для безпеки
                            fi

                            # Шукати Go файли, Docker файли, Caddyfile, HTML
                            echo "$CHANGED_FILES" | grep -E '(\\.(go|mod|sum|html)$|Dockerfile|docker-compose\\.yml|Caddyfile)' | grep -v -E '_test\\.go$' || exit 1
                        ''',
                        returnStatus: true
                    )

                    if (criticalFilesChanged == 0) {
                        echo "🔧 Critical files changed, will rebuild and restart"
                        env.RESTART_SERVICES = 'true'
                    } else {
                        echo "📝 Only docs/tests changed, skipping rebuild"
                        env.RESTART_SERVICES = 'false'
                    }
                }
            }
        }

        stage('Deploy') {
            steps {
                // Репозиторій публічний, тому fetch/pull на сервері не потребують
                // автентифікації. Якщо він стане приватним — повернути сюди
                // withCredentials і передавати креденшел через credential.helper,
                // а не в URL ремоуту (інакше він осяде в .git/config на сервері).
                sshagent(['deploy-ssh']) {
                    sh """
                        ssh -p $DEPLOY_PORT -o StrictHostKeyChecking=no $DEPLOY_USER@$DEPLOY_HOST "
                            set -e
                            echo 'Navigating to project directory...'
                            cd $DEPLOY_PATH

                            echo 'Configuring Git remote...'
                            git remote set-url origin https://github.com/bgpntx/ipv6test.git

                            echo 'Pulling changes for branch $DEPLOY_BRANCH...'
                            git fetch origin
                            git checkout $DEPLOY_BRANCH
                            git checkout .
                            git clean -fd
                            git pull origin $DEPLOY_BRANCH

                            # Умовний рестарт сервісів
                            if [ '${RESTART_SERVICES}' = 'true' ]; then
                                echo 'Rebuilding and restarting Docker containers...'
                                docker compose down --remove-orphans 2>/dev/null || true
                                docker compose up -d --build
                                echo 'Docker containers restarted successfully'
                            else
                                echo '⏭️ Skipping service restart (only non-critical files changed)'
                            fi

                            echo 'Deploy Finished Successfully!'
                        "
                    """
                }
            }
        }
    }

    post {
        success {
            script {
                def restartNote = env.RESTART_SERVICES == 'true' ?
                    '✅ Docker containers rebuilt and restarted' :
                    '⏭️ Docker containers not restarted (only docs/tests changed)'

                def payload = groovy.json.JsonOutput.toJson([
                    embeds: [[
                        title: "ipv6test Update Success",
                        description: ":white_check_mark: **Update to ${env.DEPLOY_HOST} (ipv6test) was successful!**\n${restartNote}",
                        url: env.BUILD_URL,
                        color: 3066993,
                        footer: [text: "Jenkins Build #${env.BUILD_NUMBER}"]
                    ]]
                ])
                writeFile file: 'discord.json', text: payload
                sh 'curl -sS -H "Content-Type: application/json" --data @discord.json "$DISCORD_URL" > /dev/null'
            }
        }
        failure {
            script {
                def payload = groovy.json.JsonOutput.toJson([
                    embeds: [[
                        title: "ipv6test Update Error",
                        description: ":no_entry: **Update to ${env.DEPLOY_HOST} (ipv6test) FAILED!**\nCheck logs for details.",
                        url: env.BUILD_URL,
                        color: 15158332,
                        footer: [text: "Jenkins Build #${env.BUILD_NUMBER}"]
                    ]]
                ])
                writeFile file: 'discord.json', text: payload
                sh 'curl -sS -H "Content-Type: application/json" --data @discord.json "$DISCORD_URL" > /dev/null'
            }
        }
    }
}
