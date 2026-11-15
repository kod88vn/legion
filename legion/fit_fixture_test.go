package main

// The real rAthena rows the hunt-fit tests read, copied read-only from the ro-rathena server's db/re/item_db_etc.yml
// (the fit tests load them through loadItemDB / loadBonusFile, so the names and scripts are the server's own).
// gocheck copies only *.go files, so the rows live here and not in testdata/.

const fitAmmoRowsYML = `# Arrows 1750-1757 (db/re/item_db_etc.yml)
  - Id: 1750
    AegisName: Arrow
    Name: Arrow
    Type: Ammo
    SubType: Arrow
    Buy: 1
    Sell: 0
    Weight: 1
    Attack: 25
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1

  - Id: 1751
    AegisName: Silver_Arrow
    Name: Silver Arrow
    Type: Ammo
    SubType: Arrow
    Buy: 5
    Sell: 0
    Weight: 1
    Attack: 30
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1
    Script: |
      bonus bAtkEle,Ele_Holy;

  - Id: 1752
    AegisName: Fire_Arrow
    Name: Fire Arrow
    Type: Ammo
    SubType: Arrow
    Buy: 5
    Sell: 0
    Weight: 1
    Attack: 30
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1
    Script: |
      bonus bAtkEle,Ele_Fire;

  - Id: 1753
    AegisName: Steel_Arrow
    Name: Steel Arrow
    Type: Ammo
    SubType: Arrow
    Buy: 4
    Sell: 0
    Weight: 1
    Attack: 40
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1

  - Id: 1754
    AegisName: Crystal_Arrow
    Name: Crystal Arrow
    Type: Ammo
    SubType: Arrow
    Buy: 5
    Sell: 0
    Weight: 1
    Attack: 30
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1
    Script: |
      bonus bAtkEle,Ele_Water;

  - Id: 1755
    AegisName: Arrow_Of_Wind
    Name: Arrow of Wind
    Type: Ammo
    SubType: Arrow
    Buy: 5
    Sell: 0
    Weight: 1
    Attack: 30
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1
    Script: |
      bonus bAtkEle,Ele_Wind;

  - Id: 1756
    AegisName: Stone_Arrow
    Name: Stone Arrow
    Type: Ammo
    SubType: Arrow
    Buy: 5
    Sell: 0
    Weight: 1
    Attack: 30
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1
    Script: |
      bonus bAtkEle,Ele_Earth;

  - Id: 1757
    AegisName: Immatrial_Arrow
    Name: Immaterial Arrow
    Type: Ammo
    SubType: Arrow
    Buy: 5
    Sell: 0
    Weight: 1
    Attack: 30
    Jobs:
      Archer: true
      Assassin: true
      BardDancer: true
      Hunter: true
      Rogue: true
      Thief: true
    Locations:
      Ammo: true
    EquipLevelMin: 1
    Script: |
      bonus bAtkEle,Ele_Ghost;
`

const fitCardRowsYML = `# Hydra Card 4035, Z Stone 9 4966, Ice Crystal Orb 312087 (db/re/item_db_etc.yml)
  - Id: 4035
    AegisName: Hydra_Card
    Name: Hydra Card
    Type: Card
    Buy: 20
    Weight: 10
    Locations:
      Right_Hand: true
    Flags:
      BuyingStore: true
      DropEffect: CLIENT
    Script: |
      bonus2 bAddRace,RC_DemiHuman,20;
      bonus2 bAddRace,RC_Player_Human,20;

  - Id: 4966
    AegisName: Z_Stone_9
    Name: Scorpio
    Type: Card
    SubType: Enchant
    Script: |
      bonus2 bAddRace,RC_All,1;

  - Id: 312087
    AegisName: Ice_C_Orb_P_P
    Name: Glacier Flower Meow Power (Physical Power)
    Type: Card
    SubType: Enchant
    Script: |
      bonus2 bAddEle,Ele_All,4;
      bonus bPAtk,3;
`
