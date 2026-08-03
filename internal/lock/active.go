// 本文件通过尝试获取同一把锁判断同步 worker 是否正在运行。
package lock

func Active(name string) (bool, error) {
	instanceLock, acquired, err := Acquire(name)
	if err != nil {
		return false, err
	}
	if !acquired {
		return true, nil
	}
	return false, instanceLock.Release()
}
