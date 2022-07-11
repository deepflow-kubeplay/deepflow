package kubernetes

import (
	"sync"
	"time"

	"github.com/op/go-logging"
	"gorm.io/gorm"

	. "github.com/metaflowys/metaflow/server/controller/common"
	models "github.com/metaflowys/metaflow/server/controller/db/mysql"
	"github.com/metaflowys/metaflow/server/controller/model"
	"github.com/metaflowys/metaflow/server/controller/service"
	"github.com/metaflowys/metaflow/server/controller/trisolaris/config"
	"github.com/metaflowys/metaflow/server/controller/trisolaris/dbmgr"
)

var log = logging.MustGetLogger("trisolaris.kubernetes")

type KubernetesInfo struct {
	mutex             sync.RWMutex
	clusterIDToDomain map[string]string
	db                *gorm.DB
	cfg               *config.Config
}

func NewKubernetesInfo(db *gorm.DB, cfg *config.Config) *KubernetesInfo {
	DomainMgr := dbmgr.DBMgr[models.Domain](db)
	dbDomains, _ := DomainMgr.GetBatchFromTypes([]int{KUBERNETES})
	clusterIDToDomain := make(map[string]string)
	for _, dbDomain := range dbDomains {
		clusterIDToDomain[dbDomain.ClusterID] = dbDomain.Lcuuid
	}

	return &KubernetesInfo{clusterIDToDomain: clusterIDToDomain, cfg: cfg, db: db}
}

func (k *KubernetesInfo) CacheClusterID(clusterID string) {
	log.Infof("start cache cluster_id (%s)", clusterID)
	k.mutex.Lock()
	_, ok := k.clusterIDToDomain[clusterID]
	if !ok {
		k.clusterIDToDomain[clusterID] = ""
		log.Infof("cache cluster_id (%s)", clusterID)
		go func() {
			for k.clusterIDToDomain[clusterID] == "" {
				domainLcuuid, err := k.createDomain(clusterID)
				if err != nil {
					log.Errorf("auto create domain failed: %v", err)
					time.Sleep(time.Second * 30)
				} else {
					k.clusterIDToDomain[clusterID] = domainLcuuid
				}
			}
		}()
	}
	k.mutex.Unlock()
	return
}

func (k *KubernetesInfo) createDomain(clusterID string) (domainLcuuid string, err error) {
	azConMgr := dbmgr.DBMgr[models.AZControllerConnection](k.db)
	azConn, err := azConMgr.GetFromControllerIP(k.cfg.NodeIP)
	if err != nil {
		log.Errorf("controller (%s) az connection not in DB", k.cfg.NodeIP)
		return
	}
	domainConf := map[string]interface{}{
		"controller_ip":              k.cfg.NodeIP,
		"pod_net_ipv4_cidr_max_mask": 16,
		"pod_net_ipv6_cidr_max_mask": 64,
		"port_name_regex":            DEFAULT_PORT_NAME_REGEX,
		"region_uuid":                azConn.Region,
	}
	domainCreate := model.DomainCreate{
		Name:                "METAFLOW_K8S_CLUSTER-" + k.cfg.NodeIP,
		Type:                KUBERNETES,
		KubernetesClusterID: clusterID,
		ControllerIP:        k.cfg.NodeIP,
		Config:              domainConf,
	}
	domain, err := service.CreateDomain(domainCreate)
	if err != nil {
		return
	}
	return domain.Lcuuid, nil
}