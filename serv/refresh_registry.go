package serv

import (
	"strconv"
	"sync"

	"code.google.com/p/log4go"
	"github.com/valyala/fasthttp"
	"github.com/wanghongfei/go-eureka-client/eureka"
	"github.com/wanghongfei/gogate/discovery"
	"github.com/wanghongfei/gogate/utils"
)

// 向eureka查询注册列表, 刷新本地列表
func refreshRegistry(serv *Server) error {
	apps, err := discovery.QueryAll()
	if nil != err {
		return err
	}
	log4go.Info("total app count: %d", len(apps))

	if nil == apps {
		log4go.Error("no service found")
		return nil
	}

	newRegistryMap := convertToMap(apps)
	log4go.Info("refreshing registry")

	refreshRegistryMap(serv, newRegistryMap)
	log4go.Info("refreshing clients")
	refreshClients(serv)

	return nil
}

// 刷新HttpClient
func refreshClients(serv *Server) error {
	if nil == serv.proxyClients {
		serv.proxyClients = new(sync.Map)
	}

	changeCount := 0
	newCount := 0
	// 遍历注册列表
	serv.registryMap.Range(func(key, val interface{}) bool {
		name := key.(string)
		hosts := val.([]string)

		client, exist := serv.proxyClients.Load(name)
		// 如果注册表中的service不存在Client
		// 则为此服务创建Client
		if !exist {
			log4go.Debug("create new client for service: %s", name)
			// 此service不存在, 创建新的
			newClient := &fasthttp.LBClient{
				Clients: createClients(hosts),
			}

			serv.proxyClients.Store(name, newClient)
			newCount++

		} else {
			// service存在
			// 对比是否有变化
			changed := isHostsChanged(client.(*fasthttp.LBClient), hosts)
			if changed {
				// 发生了变化
				// 创建新的LBClient替换掉老的
				log4go.Debug("service %s changed", name)
				newClient := &fasthttp.LBClient{
					Clients: createClients(hosts),
				}

				serv.proxyClients.Store(name, newClient)
				changeCount++
			}
		}

		return true
	})

	log4go.Info("%d services updated, %d services created", changeCount, newCount)
	return nil
}

// 对比LBClient中的host与注册列表中的host有没有变化
// 返回true表示有变化
func isHostsChanged(lbClient *fasthttp.LBClient, newHosts []string) bool {
	if len(lbClient.Clients) != len(newHosts) {
		return true
	}

	// 遍历LBClient里的每一个Client对象
	for _, client := range lbClient.Clients {
		c := client.(*fasthttp.HostClient)

		// 判断此Client的地址在不在newHosts中
		match := false
		for _, h := range newHosts {
			if h == c.Addr {
				match = true
				break
			}
		}

		// 有一个不存在的, 就认为发生了变化
		if !match {
			return true
		}
	}

	return false
}

// 为每一个host创建一个HostClient
func createClients(hosts[] string) []fasthttp.BalancingClient {
	var cs []fasthttp.BalancingClient
	for _, host := range hosts {
		client := &fasthttp.HostClient{
			Addr: host,
		}

		cs = append(cs, client)
	}

	return cs
}

// 将新服务列表保存为map
func convertToMap(apps []eureka.Application) *sync.Map {
	newAppsMap := new(sync.Map)
	for _, app := range apps {
		// 服务名
		servName := app.Name

		// 遍历每一个实例
		var instances []string
		for _, ins := range app.Instances {
			if nil == ins.Port {
				continue
			}

			instances = append(instances, ins.HostName + ":" + strconv.Itoa(ins.Port.Port))
		}

		newAppsMap.Store(servName, instances)
	}

	return newAppsMap
}

func refreshRegistryMap(s *Server, newRegistry *sync.Map) {
	if nil == s.registryMap {
		s.registryMap = new(sync.Map)
	}

	exclusiveKeys, _ := utils.FindExclusiveKey(s.registryMap, newRegistry)
	utils.DelKeys(s.registryMap, exclusiveKeys)
	utils.MergeSyncMap(newRegistry, s.registryMap)
}