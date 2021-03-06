package namenode

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	pb "github.com/litneet64/lab-2-squid-game/protogrpc"
	"google.golang.org/grpc"
)

// DEBUG TESTING --
type DebugLogger struct {
	fileName    string
	initialized bool
}

func InitLogger(fileName string) {
	dlogger.fileName = fileName
	dlogger.initialized = true
}

func DebugLog(msg ...string) {
	if !dlogger.initialized {
		log.Fatalf("[DebugLog] logger was not initialized")
	}

	f, err := os.OpenFile(dlogger.fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	FailOnError(err, fmt.Sprintf("[InitLogger] Could not open file \"%s\": %v", dlogger.fileName, err))
	defer f.Close()

	logger := log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	logger.Println(strings.Join(msg, " "))
}

func DebugLogf(msg string, a ...interface{}) {
	if !dlogger.initialized {
		log.Fatalf("[DebugLogf] logger was not initialized")
	}

	f, err := os.OpenFile(dlogger.fileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	FailOnError(err, fmt.Sprintf("[InitLogger] Could not open file \"%s\": %v", dlogger.fileName, err))
	defer f.Close()

	logger := log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	logger.Println(fmt.Sprintf(msg, a...))
}

func FailOnError(err error, msg string) {
	if err != nil {
		DebugLogf("[Fatal] %s: %v", msg, err)
		log.Fatalf("[Fatal] %s: %v", msg, err)
	}
}

var dlogger DebugLogger

// DEBUG TESTING --

type RoundInfo struct {
	playerId   uint32
	playerMove uint32
}

type Client struct {
	id     uint32
	addr   string
	client *pb.DataRegistryServiceClient
	conn   *grpc.ClientConn
	ctx    *context.Context
}

type server struct {
	pb.UnimplementedDataRegistryServiceServer
}

const (
	bindAddrEnv  = "NAMENODE_BIND_ADDR"
	dataAddrEnv1 = "DATANODE_ADDR_1"
	dataAddrEnv2 = "DATANODE_ADDR_2"
	dataAddrEnv3 = "DATANODE_ADDR_3"
)

var (
	datanodeAddr                    = [3]string{}
	clients                         = [3]Client{}
	bindAddr                        string
	dataAddr1, dataAddr2, dataAddr3 string
)

// Save the given round info into a txt file of the given datanode
func RegisterRoundMoves(client pb.DataRegistryServiceClient, stage *uint32, round *uint32, roundInfo *[]RoundInfo) {
	DebugLogf("\t[RegisterRoundMoves] Running function: RegisterRoundMoves(client, stage:%d, round:%d, roundInfo[])", *stage, *round)
	// Start timed context
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Store each move, using `roundInfo`
	all_moves := make([]*pb.PlayersMoves_Move, len(*roundInfo))

	for i := 0; i < len(*roundInfo); i++ {
		all_moves[i] = &pb.PlayersMoves_Move{
			PlayerId:   &(*roundInfo)[i].playerId,
			PlayerMove: &(*roundInfo)[i].playerMove,
		}
	}

	// Send message to datanode
	client.TransferPlayerMoves(ctx,
		&pb.PlayersMoves{
			Stage:        stage,
			Round:        round,
			PlayersMoves: all_moves,
		})
}

// Recieve player history request from leader
func (s *server) GetPlayerHistory(ctx context.Context, in *pb.PlayerHistoryRequest) (*pb.StageData, error) {
	DebugLogf("\t[server:GetPlayerHistory] Running function: GetPlayerHistory(ctx, in: %s)", in.String())
	// send player
	playerId := in.GetPlayerId()
	playerMoves := RetrievePlayerData(&playerId)

	reply := &pb.StageData{PlayerMoves: playerMoves}

	return reply, nil
}

// Recieves all the moves that a player has made.
func RetrievePlayerData(player *uint32) []uint32 {
	DebugLogf("\t[RetrievePlayerData] Running function: RetrievePlayerData(player:%d)", *player)
	var requestQueue []*Client
	var playerMoves []uint32

	// Map each address to the corresponding client object
	addrToClient := make(map[string]*Client, 3)
	for i := 0; i < 3; i++ {
		addrToClient[clients[i].addr] = &clients[i]
	}

	// For each stage, get if there is an address associated to
	// moves of the player
	for i := 0; i < 3; i++ {
		ui := uint32(i)
		addr, err := GetMoveLocations(player, &ui)

		if err == nil {
			requestQueue = append(requestQueue, addrToClient[addr])

		} else {
			break
		}
	}

	// Start timed context
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	for i := 0; i < len(requestQueue); i++ {
		stage := uint32(i)

		// Request to datanode and parse output
		dataResp, _ := (*requestQueue[i].client).RequestPlayerData(ctx,
			&pb.DataRequestParams{
				PlayerId: player,
				Stage:    &stage,
			})

		// 'data' should be sent to leader
		data := dataResp.GetPlayerMoves()
		playerMoves = append(playerMoves, data...)
	}

	return playerMoves
}

// Saves node locations of player moves for each stage
func SaveMoveLocations(player *uint32, stage *uint32, address *string) {
	DebugLogf("\t[SaveMoveLocations] Running function: SaveMoveLocations(player:%d, stage:%d, address:%s)", *player, *stage, *address)

	f, err := os.OpenFile("tablemap.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	FailOnError(err, "can't open file \"tablemap.txt\"")
	defer f.Close()

	locationtemplate := "Jugador_%d Ronda_%d %v\n"
	f.WriteString(fmt.Sprintf(locationtemplate, player, stage, address))
	f.Sync()
}

// Returns datanode address where player moves for a stage are located,
// return empty string if not found
func GetMoveLocations(player *uint32, stage *uint32) (string, error) {
	DebugLogf("\t[GetMoveLocations] Running function: GetMoveLocations(player:%d, stage:%d)", *player, *stage)
	// Checks if save file exists
	_, fErr := os.Stat("tablemap.txt")
	if fErr != nil {
		return "", fErr
	}

	// Open savefile
	f, err := os.Open("tablemap.txt")
	FailOnError(err, "can't open file \"tablemap.txt\"")
	defer f.Close()

	// reads each line and checks if it has requested player and stage
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		saveData := strings.Split(scanner.Text(), " ")

		samePlayer := saveData[0] == fmt.Sprintf("Jugador_%d", player)
		sameStage := saveData[1] == fmt.Sprintf("Ronda_%d", stage)

		if samePlayer && sameStage {
			return saveData[2], nil
		}
	}
	return "", os.ErrNotExist
}

func Namenode_go() {
	InitLogger("namenode.log")

	bindAddr = os.Getenv(bindAddrEnv)
	dataAddr1 = os.Getenv(dataAddrEnv1)
	dataAddr2 = os.Getenv(dataAddrEnv2)
	dataAddr3 = os.Getenv(dataAddrEnv3)

	// Define arrays of both connections and errors for each of the
	// three datanodes that are connected to the namenode

	var conns [3]*grpc.ClientConn
	var errs [3]error
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Dial each datanode
	for i := 0; i < 3; i++ {
		conns[i], errs[i] = grpc.Dial(datanodeAddr[i], grpc.WithInsecure())
		FailOnError(errs[i], fmt.Sprintf("[Namenode] Error connecting to datanode #%d: \"%v\"", i, errs[i]))

		client := pb.NewDataRegistryServiceClient(conns[i])
		clients[i] = Client{
			id:     uint32(i),
			addr:   datanodeAddr[i],
			client: &client,
			conn:   conns[i],
			ctx:    &ctx,
		}

		defer conns[i].Close()
	}

	DebugLog("Dialing each client (Leader and Datanodes)")

	lis, err := net.Listen("tcp", bindAddr)
	FailOnError(err, "[Namenode] failed to listen on address")

	DebugLogf("Listening on port %s", bindAddr)

	namenode_srv := grpc.NewServer()
	pb.RegisterDataRegistryServiceServer(namenode_srv, &server{})
	DebugLog("Running new grpc server")

	if err := namenode_srv.Serve(lis); err != nil {
		log.Fatalf("[Namenode] Could not bind to %v : %v", bindAddr, err)
	}
}
