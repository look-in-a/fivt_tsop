package mocker

import (
	"fmt"
)

func checkNumberOfArgsEqual(args []string, num int) {
	if len(args) != num {
		panic("Wrong number of args. Use 'mocker help' for detailed info")
	}
}
func checkNumberOfArgsNotLess(args []string, num int) {
	if len(args) < num {
		panic("Wrong number of args. Use 'mocker help' for detailed info")
	}
}

type Mocker struct {
	Commands map[string]func([]string)
	usage    []string
}

func (m *Mocker) add(name string, act func([]string), usage string) {
	m.Commands[name] = act
	if usage != "" {
		m.usage = append(m.usage, usage)
	}
}

func InitMocker() Mocker {
	mocker := Mocker{Commands: make(map[string]func([]string))}

	mocker.add("init", func(args []string) {
		checkNumberOfArgsEqual(args, 1)
		mocker.init(args[0])
	}, "init <directory> - создать образ контейнера используя указанную директорию как корневую. Возвращает в stdout id созданного образа.")

	mocker.add("pull", func(args []string) {
		checkNumberOfArgsEqual(args, 1)
		mocker.pull(args[0])
	}, "pull <image> - скачать последний (latest) тег указанного образа с Docker Hub. Возвращает в stdout id созданного образа.")

	mocker.add("rmi", func(args []string) {
		checkNumberOfArgsEqual(args, 1)
		mocker.rmi(args[0])
	}, "rmi <image_id> - удаляет ранее созданный образ из локального хранилища.")

	mocker.add("images", func(args []string) {
		checkNumberOfArgsEqual(args, 0)
		mocker.images()
	}, "images - выводит список локальных образов")

	mocker.add("ps", func(args []string) {
		checkNumberOfArgsEqual(args, 0)
		mocker.ps()
	}, "ps - выводит список контейнеров")

	mocker.add("run", func(args []string) {
		checkNumberOfArgsNotLess(args, 2)
		mocker.run(args[0], args[1:]...)
	}, "run <image_id> <command> - создает контейнер из указанного image_id и запускает его с указанной командой")

	mocker.add("exec", func(args []string) {
		checkNumberOfArgsNotLess(args, 2)
		mocker.exec(args[0], args[1:]...)
	}, "exec <container_id> <command> - запускает указанную команду внутри уже запущенного указанного контейнера")

	mocker.add("logs", func(args []string) {
		checkNumberOfArgsEqual(args, 1)
		mocker.logs(args[0])
	}, "logs <container_id> - выводит логи указанного контейнера")

	mocker.add("rm", func(args []string) {
		checkNumberOfArgsEqual(args, 1)
		mocker.rm(args[0])
	}, "rm <container_id> - удаляет ранее созданный контейнер")

	mocker.add("commit", func(args []string) {
		checkNumberOfArgsEqual(args, 2)
		mocker.commit(args[0], args[1])
	}, "commit <container_id> <image_id> - создает новый образ, применяя изменения из образа container_id к образу image_id")

	mocker.add("help", func(args []string) {
		checkNumberOfArgsEqual(args, 0)
		fmt.Println()
		for _, usage := range mocker.usage {
			fmt.Println("\t•", usage)
			fmt.Println()
		}
	}, "help выводит подсказки по командам")

	mocker.add("rerun", func(args []string) {
		rerun(args)
		return
	}, "")

	mocker.add("reexec", func(args []string) {
		reexec(args)
		return
	}, "")
	return mocker
}
