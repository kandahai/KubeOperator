package model

import (
	"github.com/KubeOperator/KubeOperator/pkg/constant"
	"github.com/KubeOperator/KubeOperator/pkg/db"
	"github.com/KubeOperator/KubeOperator/pkg/model/common"
	"github.com/KubeOperator/KubeOperator/pkg/service/cluster/adm/facts"
	"github.com/KubeOperator/kobe/api"
	uuid "github.com/satori/go.uuid"
)

type Cluster struct {
	common.BaseModel
	ID       string        `json:"_"`
	Name     string        `json:"name" gorm:"not null;unique"`
	Source   string        `json:"source"`
	SpecID   string        `json:"_"`
	SecretID string        `json:"_"`
	StatusID string        `json:"_"`
	PlanID   string        `json:"_"`
	Plan     Plan          `json:"_"`
	Spec     ClusterSpec   `gorm:"save_associations:false" json:"spec"`
	Secret   ClusterSecret `gorm:"save_associations:false" json:"_"`
	Status   ClusterStatus `gorm:"save_associations:false" json:"_"`
	Nodes    []ClusterNode `gorm:"save_associations:false" json:"_"`
}

func (c Cluster) TableName() string {
	return "ko_cluster"
}

func (c *Cluster) BeforeCreate() error {
	c.ID = uuid.NewV4().String()
	tx := db.DB.Begin()
	if err := tx.Create(&c.Spec).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Create(&c.Status).Error; err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Create(&c.Secret).Error; err != nil {
		tx.Rollback()
		return err
	}
	c.SpecID = c.Spec.ID
	c.StatusID = c.Status.ID
	c.SecretID = c.Secret.ID
	for i, _ := range c.Nodes {
		c.Nodes[i].ClusterID = c.ID
		if err := tx.Create(&c.Nodes[i]).Error; err != nil {
			tx.Rollback()
			return err
		}
		c.Nodes[i].Host.ClusterID = c.ID
		err := tx.Save(&Host{ID: c.Nodes[i].HostID, ClusterID: c.ID}).Error
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	for _, tool := range c.PrepareTools() {
		tool.ClusterID = c.ID
		err := tx.Create(&tool).Error
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	tx.Commit()
	return nil
}

func (c Cluster) BeforeDelete() error {
	var cluster Cluster
	cluster.ID = c.ID
	if err := db.DB.
		Preload("Status").
		Preload("Spec").
		Preload("Nodes").
		First(&cluster).Error; err != nil {
		return err
	}
	tx := db.DB.Begin()
	if cluster.SpecID != "" {
		if err := db.DB.Delete(ClusterSpec{ID: cluster.SpecID}).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	if cluster.StatusID != "" {
		if err := tx.Delete(ClusterStatus{ID: cluster.StatusID}).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	if cluster.SecretID != "" {
		if err := db.DB.Delete(ClusterSecret{ID: cluster.SecretID}).Error; err != nil {
			tx.Rollback()
			return err
		}
	}
	if len(cluster.Nodes) > 0 {
		for _, node := range cluster.Nodes {
			if err := tx.Where(ClusterNode{ID: node.ID}).
				Delete(ClusterNode{}).Error; err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	tx.Commit()
	return nil
}

func (c Cluster) PrepareTools() []ClusterTool {
	return []ClusterTool{
		{
			Name:     "prometheus",
			Version:  "v1.0.0",
			Describe: "",
			Status:   constant.ClusterWaiting,
			Logo:     "prometheus.png",
		},
		{
			Name:     "dashboard",
			Version:  "v1.0.0",
			Describe: "",
			Status:   constant.ClusterWaiting,
			Logo:     "kubernetes.png",
		},
		{
			Name:     "chartmuseum",
			Version:  "v1.0.0",
			Describe: "",
			Status:   constant.ClusterWaiting,
			Logo:     "chartmuseum.png",
		},
		{
			Name:     "registry",
			Version:  "v1.0.0",
			Describe: "",
			Status:   constant.ClusterWaiting,
			Logo:     "registry.png",
		},
	}
}

func (c Cluster) GetKobeVars() map[string]string {
	result := map[string]string{}
	if c.Spec.NetworkType != "" {
		result[facts.NetworkPluginFactName] = c.Spec.NetworkType
	}
	if c.Spec.RuntimeType != "" {
		result[facts.ContainerRuntimeFactName] = c.Spec.RuntimeType
	}
	if c.Spec.DockerStorageDir != "" {
		result[facts.DockerStorageDirFactName] = c.Spec.DockerStorageDir
	}
	if c.Spec.ContainerdStorageDir != "" {
		result[facts.ContainerdStorageDirFactName] = c.Spec.ContainerdStorageDir
	}
	if c.Spec.LbKubeApiserverIp != "" {
		result[facts.LbKubeApiserverPortFactName] = c.Spec.LbKubeApiserverIp
	}
	if c.Spec.KubePodSubnet != "" {
		result[facts.KubePodSubnetFactName] = c.Spec.KubePodSubnet
	}
	if c.Spec.KubeServiceSubnet != "" {
		result[facts.KubeServiceSubnetFactName] = c.Spec.KubeServiceSubnet
	}
	return result
}

func (c Cluster) ParseInventory() api.Inventory {
	var masters []string
	var workers []string
	var chrony []string
	var hosts []*api.Host
	for _, node := range c.Nodes {
		hosts = append(hosts, node.ToKobeHost())
		switch node.Role {
		case constant.NodeRoleNameMaster:
			if node.Status == "" || node.Status == constant.ClusterRunning {
				masters = append(masters, node.Name)
			}
		case constant.NodeRoleNameWorker:
			if node.Status == "" || node.Status == constant.ClusterRunning {
				workers = append(workers, node.Name)
			}
		}
	}
	if len(masters) > 0 {
		chrony = append(chrony, masters[0])
	}
	return api.Inventory{
		Hosts: hosts,
		Groups: []*api.Group{
			{
				Name:     "kube-master",
				Hosts:    masters,
				Children: []string{},
				Vars:     map[string]string{},
			},
			{
				Name:  "kube-worker",
				Hosts: workers,
				Children: []string{
					"kube-master",
				},
				Vars: map[string]string{},
			},
			{
				Name:  "new-worker",
				Hosts: []string{},
				Vars:  map[string]string{},
			}, {

				Name:     "lb",
				Hosts:    []string{},
				Children: []string{},
				Vars:     map[string]string{},
			},
			{
				Name:     "etcd",
				Hosts:    masters,
				Children: []string{"kube-master"},
				Vars:     map[string]string{},
			}, {
				Name:     "chrony",
				Hosts:    chrony,
				Children: []string{},
				Vars:     map[string]string{},
			},
			{
				Name:     "del-worker",
				Hosts:    []string{},
				Children: []string{},
				Vars:     map[string]string{},
			},
		},
	}
}